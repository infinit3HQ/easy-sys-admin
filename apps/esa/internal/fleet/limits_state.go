package fleet

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// Per-account caps on stored items.
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
// notification queue, and poll back-off. It lives beside the Service rather
// than in it, keyed by the Service pointer.
type runtimeState struct {
	rateMu sync.Mutex
	rates  map[string]*rateBucket

	notifyOnce sync.Once
	notifyQ    chan notifyJob

	backoffMu sync.Mutex
	backoff   map[int64]*pollBackoff
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

type notifyJob struct {
	ch  store.NotifyChannel
	msg Message
}

// enqueueNotify hands one delivery to the background workers, so a slow or
// hanging channel never holds up polling. When the queue is full the
// message is dropped with a warning.
func (s *Service) enqueueNotify(ch store.NotifyChannel, msg Message) {
	rt := s.rt()
	rt.notifyOnce.Do(func() {
		rt.notifyQ = make(chan notifyJob, notifyQueueSize)
		for range notifyWorkers {
			go s.notifyWorker(rt.notifyQ)
		}
	})
	select {
	case rt.notifyQ <- notifyJob{ch: ch, msg: msg}:
	default:
		s.log.Warn("notification queue full; dropping message", "account_id", ch.AccountID, "channel", ch.Name)
	}
}

func (s *Service) notifyWorker(q <-chan notifyJob) {
	for {
		select {
		case <-s.baseCtx.Done():
			return
		case j := <-q:
			// sendOne bounds each delivery to 20 seconds and logs failures.
			_ = s.sendOne(s.baseCtx, j.ch, j.msg)
		}
	}
}

// ---- Poll back-off -----------------------------------------------------------

// pollBackoff counts consecutive SSH authentication failures for a server.
// Repeated failed logins can get esa's address banned (fail2ban), so such
// servers are polled less often.
type pollBackoff struct {
	failures int
	next     time.Time
}

const maxPollBackoff = 30 * time.Minute

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

// pollDeferred reports whether scheduled polling of a server is backing off.
func (s *Service) pollDeferred(serverID int64) bool {
	rt := s.rt()
	rt.backoffMu.Lock()
	defer rt.backoffMu.Unlock()
	b := rt.backoff[serverID]
	return b != nil && time.Now().Before(b.next)
}
