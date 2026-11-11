package fleet

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// Per-account caps on stored items. maxServers is the default of
// ESA_MAX_SERVERS_PER_ACCOUNT; Service.maxServers holds the setting.
const (
	maxChannels = 25
	maxCerts    = 200
	maxServers  = 500
)

// Rate limit for actions that make esa connect somewhere on request.
const (
	rateLimit  = 20
	rateWindow = time.Minute
)

// runtimeState is in-memory state kept per Service: rate limits, the
// notification queue, poll back-off, and poll statistics. It lives beside
// the Service rather than in it, keyed by the Service pointer.
type runtimeState struct {
	rateMu sync.Mutex
	rates  map[string]*rateBucket

	notifyOnce    sync.Once
	notifyQ       chan int64
	notifyQueue   atomic.Pointer[chan int64] // notifyQ once created, for Stats
	notifyDropped atomic.Uint64

	backoffMu sync.Mutex
	backoff   map[int64]*pollBackoff
	// cycleStart is when the current or last PollAll began. Back-off for
	// unreachable servers counts whole cycles from it.
	cycleStart time.Time

	statsMu sync.Mutex
	stats   PollStats
}

var runtimeStates sync.Map // *Service -> *runtimeState

func (s *Service) rt() *runtimeState {
	if v, ok := runtimeStates.Load(s); ok {
		return v.(*runtimeState)
	}
	v, _ := runtimeStates.LoadOrStore(s, &runtimeState{
		rates:   map[string]*rateBucket{},
		backoff: map[int64]*pollBackoff{},
	})
	return v.(*runtimeState)
}

type rateBucket struct {
	start time.Time
	n     int
}

// limitRate allows rateLimit calls of one action per account per minute.
func (s *Service) limitRate(a Actor, action string) error {
	if a.Source == SourceSystem {
		return nil
	}
	rt := s.rt()
	now := time.Now()
	key := strconv.FormatInt(a.AccountID, 10) + "|" + action
	rt.rateMu.Lock()
	defer rt.rateMu.Unlock()
	if len(rt.rates) > 10000 {
		for k, b := range rt.rates {
			if now.Sub(b.start) >= rateWindow {
				delete(rt.rates, k)
			}
		}
	}
	b := rt.rates[key]
	if b == nil || now.Sub(b.start) >= rateWindow {
		b = &rateBucket{start: now}
		rt.rates[key] = b
	}
	if b.n >= rateLimit {
		return &ConflictError{Message: "too many requests; wait a minute and try again"}
	}
	b.n++
	return nil
}

// underCap returns a ConflictError when the account already has max rows of model.
func (s *Service) underCap(ctx context.Context, a Actor, model any, max int, what string) error {
	var n int64
	if err := s.scoped(ctx, a).Model(model).Count(&n).Error; err != nil {
		return err
	}
	if n >= int64(max) {
		return &ConflictError{Message: "this account has reached the limit of " + strconv.Itoa(max) + " " + what + "; delete one first"}
	}
	return nil
}

// ---- Notification queue ----------------------------------------------------

const (
	notifyQueueSize = 1000
	notifyWorkers   = 4
)

// enqueueNotify stores one delivery, then hands its id to the background
// workers. A full queue leaves the row queued. DrainDeliveries sends it
// after a restart or when the queue was full.
func (s *Service) enqueueNotify(ch store.NotifyChannel, msg Message) {
	row := store.NotifyDelivery{
		AccountID: ch.AccountID, ChannelID: ch.ID, Title: msg.Title, Body: msg.Body,
		Severity: msg.Severity, URL: msg.URL, State: "queued", CreatedAt: s.now(),
	}
	if err := s.db.WithContext(context.WithoutCancel(s.baseCtx)).Omit("Account", "Channel").Create(&row).Error; err != nil {
		s.log.Error("store notification", "account_id", ch.AccountID, "channel", ch.Name, "err", err)
		return
	}
	s.kickNotify(row.ID)
}

func (s *Service) kickNotify(id int64) {
	rt := s.rt()
	rt.notifyOnce.Do(func() {
		rt.notifyQ = make(chan int64, notifyQueueSize)
		rt.notifyQueue.Store(&rt.notifyQ)
		for range notifyWorkers {
			go s.notifyWorker(rt.notifyQ)
		}
	})
	select {
	case rt.notifyQ <- id:
	default:
		rt.notifyDropped.Add(1)
		s.log.Warn("notification workers busy; the message stays queued", "delivery_id", id)
	}
}

func (s *Service) notifyWorker(q <-chan int64) {
	for {
		select {
		case <-s.baseCtx.Done():
			return
		case id := <-q:
			s.deliver(s.baseCtx, id)
		}
	}
}

const notifyMaxAttempts = 5

// DrainDeliveries sends queued notifications and returns expired snoozes
// to the review list. A delivery left in "sending" for two minutes is
// queued again.
func (s *Service) DrainDeliveries(ctx context.Context) {
	stale := s.now().Add(-2 * time.Minute)
	s.db.WithContext(ctx).Model(&store.NotifyDelivery{}).
		Where("state = ? AND created_at < ?", "sending", stale).
		Update("state", "queued")
	var ids []int64
	s.db.WithContext(ctx).Model(&store.NotifyDelivery{}).Where("state = ?", "queued").Limit(100).Pluck("id", &ids)
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		s.deliver(ctx, id)
	}
	s.db.WithContext(ctx).Model(&store.SecurityIncident{}).
		Where("state = ? AND snooze_until IS NOT NULL AND snooze_until <= ?", "snoozed", s.now()).
		Updates(map[string]any{"state": "open", "snooze_until": nil, "snooze_reason": ""})
}

func (s *Service) deliver(ctx context.Context, id int64) {
	res := s.db.WithContext(ctx).Model(&store.NotifyDelivery{}).
		Where("id = ? AND state = ?", id, "queued").
		Updates(map[string]any{"state": "sending", "attempts": gorm.Expr("attempts + 1")})
	if res.Error != nil || res.RowsAffected == 0 {
		return
	}
	var row store.NotifyDelivery
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return
	}
	var ch store.NotifyChannel
	if err := s.db.WithContext(ctx).First(&ch, row.ChannelID).Error; err != nil || !ch.Enabled {
		s.finishDelivery(ctx, id, row.Attempts, "channel is gone or disabled", true)
		return
	}
	err := s.sendOne(ctx, ch, Message{Title: row.Title, Body: row.Body, Severity: row.Severity, URL: row.URL})
	if err == nil {
		now := s.now()
		s.db.WithContext(ctx).Model(&store.NotifyDelivery{}).Where("id = ?", id).
			Updates(map[string]any{"state": "sent", "sent_at": now, "last_error": ""})
		return
	}
	s.finishDelivery(ctx, id, row.Attempts, netErrorText(err), row.Attempts >= notifyMaxAttempts)
}

func (s *Service) finishDelivery(ctx context.Context, id int64, attempts int, errText string, dead bool) {
	state := "queued"
	if dead || attempts >= notifyMaxAttempts {
		state = "dead"
		s.log.Warn("notification gave up", "delivery_id", id, "attempts", attempts, "err", errText)
	}
	s.db.WithContext(ctx).Model(&store.NotifyDelivery{}).Where("id = ?", id).
		Updates(map[string]any{"state": state, "last_error": errText})
}

// ---- Poll back-off -----------------------------------------------------------

// pollBackoff holds when a server may next be polled on schedule. Two
// things push that time out, and the later one wins:
//
//   - Consecutive SSH authentication failures. Repeated failed logins can
//     get esa's address banned (fail2ban), so such servers are polled less
//     often (backoffDelay).
//   - Being unreachable. Once a server has missed unreachableAfter polls in
//     a row it is polled once every unreachablePollEvery, so a dead server
//     does not hold a poll worker for a connect or handshake timeout every
//     minute.
//
// The state is in memory only. After a restart every server is tried once
// and the back-off starts again from its stored missed-poll count.
type pollBackoff struct {
	failures int
	next     time.Time
}

const (
	maxPollBackoff = 30 * time.Minute
	// unreachablePollEvery spaces polls of an unreachable server.
	unreachablePollEvery = 5 * time.Minute
	// cycleSlack absorbs ticker jitter, so a server backed off for five
	// cycles is polled on the fifth one, not the sixth.
	cycleSlack = 2 * time.Second
)

// backoffDelay is how long to wait after n consecutive auth failures:
// nothing for the first two, then 2, 4, 8, 16, and at most 30 minutes.
func backoffDelay(n int) time.Duration {
	if n < 3 {
		return 0
	}
	d := time.Minute << min(n-2, 6)
	return min(d, maxPollBackoff)
}

func (s *Service) recordAuthFailure(serverID int64) time.Duration {
	rt := s.rt()
	rt.backoffMu.Lock()
	defer rt.backoffMu.Unlock()
	b := rt.backoff[serverID]
	if b == nil {
		b = &pollBackoff{}
		rt.backoff[serverID] = b
	}
	b.failures++
	d := backoffDelay(b.failures)
	b.next = time.Now().Add(d)
	return d
}

func (s *Service) clearBackoff(serverID int64) {
	rt := s.rt()
	rt.backoffMu.Lock()
	delete(rt.backoff, serverID)
	rt.backoffMu.Unlock()
}

// recordUnreachable defers scheduled polls of a server that stopped
// answering for unreachablePollEvery, counted from the start of the current
// poll cycle. It keeps a later time set by the auth-failure back-off.
func (s *Service) recordUnreachable(serverID int64) {
	rt := s.rt()
	rt.backoffMu.Lock()
	defer rt.backoffMu.Unlock()
	from := rt.cycleStart
	if from.IsZero() {
		from = time.Now()
	}
	next := from.Add(unreachablePollEvery - cycleSlack)
	b := rt.backoff[serverID]
	if b == nil {
		b = &pollBackoff{}
		rt.backoff[serverID] = b
	}
	if next.After(b.next) {
		b.next = next
	}
}

// startCycle records the start of a poll cycle and returns it.
func (s *Service) startCycle() time.Time {
	rt := s.rt()
	now := time.Now()
	rt.backoffMu.Lock()
	rt.cycleStart = now
	rt.backoffMu.Unlock()
	return now
}

// pollDeferred reports whether scheduled polling of a server is backing
// off at time at, the start of the poll cycle asking.
func (s *Service) pollDeferred(serverID int64, at time.Time) bool {
	rt := s.rt()
	rt.backoffMu.Lock()
	defer rt.backoffMu.Unlock()
	b := rt.backoff[serverID]
	return b != nil && at.Before(b.next)
}
