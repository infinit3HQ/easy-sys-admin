package fleet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const (
	pollTimeout  = 45 * time.Second
	updatesEvery = 6 * time.Hour
	// Defaults of ESA_POLL_CONCURRENCY, ESA_METRICS_RAW_RETENTION, and
	// ESA_METRICS_HOURLY_RETENTION. The Service holds the settings.
	defaultPollConcurrency = 64
	defaultRawRetention    = 48 * time.Hour
	defaultHourlyRetention = 90 * 24 * time.Hour
)

// PollAll polls every ready server in every account, s.pollConcurrency at
// a time. The caller's deadline ends the cycle; servers attempted longest
// ago go first, so a cycle cut short does not starve the same servers every
// time. Servers that keep rejecting esa's login, and servers that stopped
// answering, are polled less often (see pollBackoff).
func (s *Service) PollAll(ctx context.Context) {
	if !s.claimJob(ctx, "poll", 70*time.Second) {
		return
	}
	start := s.startCycle()
	var ids []int64
	err := s.db.WithContext(ctx).Model(&store.Server{}).
		Joins("LEFT JOIN server_states ON server_states.server_id = servers.id").
		Where("servers.enrollment = 'ready'").
		Order("server_states.last_attempt_at NULLS FIRST, servers.id").
		Pluck("servers.id", &ids).Error
	if err != nil {
		s.log.Error("list servers to poll", "err", err)
		return
	}
	s.exposureBudget.Store(exposurePerCycle)
	sem := make(chan struct{}, s.pollConcurrency)
	var wg sync.WaitGroup
	skipped, deferred := 0, 0
	countRest := func(rest []int64) {
		for _, id := range rest {
			if s.pollDeferred(id, start) {
				deferred++
			} else {
				skipped++
			}
		}
	}
loop:
	for i, id := range ids {
		if s.pollDeferred(id, start) {
			deferred++
			continue
		}
		if ctx.Err() != nil {
			countRest(ids[i:])
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			// Count what this cycle had no time to start.
			countRest(ids[i:])
			break loop
		}
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			defer func() { <-sem }()
			began := time.Now()
			ok := s.pollServer(ctx, id, true)
			s.recordPoll(ok, time.Since(began))
		}(id)
	}
	wg.Wait()
	took := time.Since(start)
	s.recordCycle(took, len(ids), skipped, deferred)
	if skipped > 0 {
		s.log.Warn("poll cycle ran out of time; the rest wait for the next cycle. Raise ESA_POLL_CONCURRENCY if this repeats",
			"skipped", skipped, "servers", len(ids), "deferred", deferred, "concurrency", s.pollConcurrency, "seconds", int(took.Seconds()))
	}
}

// claimJob takes or renews a background job. A second process gets false
// until the current holder's time has passed. A nil database always claims,
// so a test with no database still runs.
func (s *Service) claimJob(ctx context.Context, name string, ttl time.Duration) bool {
	if s.db == nil {
		return true
	}
	now := s.now()
	res := s.db.WithContext(ctx).Exec(`
		INSERT INTO job_leases (name, holder, until) VALUES (?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET holder = EXCLUDED.holder, until = EXCLUDED.until
		WHERE job_leases.until < ? OR job_leases.holder = EXCLUDED.holder`,
		name, s.holder(), now.Add(ttl), now)
	if res.Error != nil {
		s.log.Error("claim job", "job", name, "err", res.Error)
		return false
	}
	return res.RowsAffected == 1
}

func (s *Service) holder() string {
	s.holderOnce.Do(func() {
		var b [8]byte
		_, _ = rand.Read(b[:])
		s.holderID = hex.EncodeToString(b[:])
	})
	return s.holderID
}

// PollServer collects one status snapshot, stores it, and evaluates alerts.
func (s *Service) PollServer(ctx context.Context, serverID int64) {
	s.pollServer(ctx, serverID, false)
}

// pollServer is PollServer. It reports whether the server answered.
// budgeted limits exposure collection to this poll cycle's cap.
func (s *Service) pollServer(ctx context.Context, serverID int64, budgeted bool) bool {
	var srv store.Server
	if err := s.db.WithContext(ctx).Where("id = ?", serverID).Take(&srv).Error; err != nil {
		return false
	}
	var state store.ServerState
	if err := s.db.WithContext(ctx).Where("server_id = ?", serverID).Take(&state).Error; err != nil {
		state = store.ServerState{ServerID: serverID}
	}
	now := s.now()
	state.LastAttemptAt = &now

	if state.HostKeyMismatch {
		// Never touch a server whose key changed until a person accepts it.
		s.saveState(ctx, &state)
		return false
	}

	ep, err := s.endpoint(ctx, srv)
	var snap proto.Status
	if err == nil {
		pctx, cancel := context.WithTimeout(s.netCtx(ctx), pollTimeout)
		err = s.pool.Call(pctx, ep, &snap, "status")
		cancel()
	}
	switch {
	case err == nil:
		s.clearBackoff(serverID)
	case errors.Is(err, remote.ErrAuthFailed):
		if d := s.recordAuthFailure(serverID); d > 0 {
			s.log.Warn("ssh login refused repeatedly; polling less often", "server_id", serverID, "next_in", d)
		}
	}
	if err != nil {
		var mismatch *remote.HostKeyMismatchError
		if errors.As(err, &mismatch) {
			s.recordMismatch(ctx, serverID, mismatch)
			s.evaluateAlerts(ctx, srv, nil)
			return false
		}
		if ctx.Err() != nil {
			return false
		}
		state.MissedPolls++
		state.LastError = err.Error()
		if state.MissedPolls >= unreachableAfter || state.LastSeenAt == nil {
			state.Status = string(StatusUnreachable)
			state.Reasons = store.MustJSON([]string{"no answer: " + err.Error()})
		}
		if state.MissedPolls >= unreachableAfter {
			s.recordUnreachable(serverID)
			if state.MissedPolls == unreachableAfter {
				s.log.Info("server not answering; polling it every 5 minutes until it does", "server_id", serverID)
			}
		}
		s.saveState(ctx, &state)
		s.evaluateAlerts(ctx, srv, &state)
		return false
	}

	var upd *proto.PendingUpdates
	if len(state.Updates) > 0 {
		upd = &proto.PendingUpdates{}
		_ = state.Updates.Decode(upd)
	}
	status, reasons := evaluate(&snap, upd)
	state.Status, state.Reasons = string(status), store.MustJSON(reasons)
	state.Snapshot = store.MustJSON(snap)
	state.LastSeenAt, state.LastError, state.MissedPolls = &now, "", 0
	s.saveState(ctx, &state)

	var rx, tx uint64
	for _, n := range snap.Net {
		rx += n.RxBytes
		tx += n.TxBytes
	}
	sample := store.MetricSample{
		ServerID: serverID, At: now.Truncate(time.Second), CPUPercent: snap.CPUPercent,
		MemPercent: round(pct(snap.MemUsed, snap.MemTotal)), DiskPercent: round(worstDisk(&snap)),
		Load1: snap.Load1, RxBytes: int64(rx), TxBytes: int64(tx),
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Omit(clause.Associations).Create(&sample).Error; err != nil {
		s.log.Warn("store metric sample", "server_id", serverID, "err", err)
	}
	s.maybeExposure(ctx, srv, &state, budgeted)
	if state.UpdatesAt == nil || now.Sub(*state.UpdatesAt) > updatesEvery {
		if s.RefreshUpdates(ctx, serverID, false) == nil {
			var fresh store.ServerState
			if s.db.WithContext(ctx).Select("updates", "security_seen").Where("server_id = ?", serverID).Take(&fresh).Error == nil {
				state.Updates, state.SecuritySeen = fresh.Updates, fresh.SecuritySeen
			}
		}
	}
	s.evaluateAlerts(ctx, srv, &state)
	return true
}

func round(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

func (s *Service) saveState(ctx context.Context, st *store.ServerState) {
	st.UpdatedAt = s.now()
	if st.Reasons == nil {
		st.Reasons = store.JSON("[]")
	}
	// Explicit columns: GORM's UpdateAll skips columns with a database
	// default (such as the jsonb reasons), and the update list is written
	// separately by RefreshUpdates and must not be overwritten here.
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "server_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"status", "reasons", "snapshot", "last_seen_at", "last_attempt_at", "last_error",
			"missed_polls", "host_key_mismatch", "presented_host_key", "updated_at",
		}),
	}).Omit(clause.Associations).Create(st).Error
	if err != nil {
		s.log.Error("save server state", "server_id", st.ServerID, "err", err)
	}
}

// recordMismatch stops all work on a server whose host key changed.
func (s *Service) recordMismatch(ctx context.Context, serverID int64, m *remote.HostKeyMismatchError) {
	s.pool.Forget(serverID)
	now := s.now()
	presented := remote.MarshalKey(m.Presented)
	err := s.db.WithContext(ctx).Model(&store.ServerState{}).Where("server_id = ?", serverID).Updates(map[string]any{
		"host_key_mismatch": true, "presented_host_key": presented, "status": string(StatusFailing),
		"reasons": store.MustJSON([]string{"host key changed"}), "last_error": m.Error(), "last_attempt_at": now, "updated_at": now,
	}).Error
	if err != nil {
		s.log.Error("record host key mismatch", "server_id", serverID, "err", err)
	}
	var srv store.Server
	if s.db.WithContext(ctx).Where("id = ?", serverID).Take(&srv).Error == nil {
		s.audit(ctx, nil, System(srv.AccountID), "server.host_key_mismatch", srv.Name, map[string]any{"presented": remote.Fingerprint(m.Presented)})
	}
	s.log.Warn("host key mismatch; server paused", "server_id", serverID, "presented", remote.Fingerprint(m.Presented))
}

// RefreshUpdates stores the pending update list. refresh runs apt-get update first.
func (s *Service) RefreshUpdates(ctx context.Context, serverID int64, refresh bool) error {
	var srv store.Server
	if err := s.db.WithContext(ctx).Where("id = ?", serverID).Take(&srv).Error; err != nil {
		return mapStore(err)
	}
	ep, err := s.endpoint(ctx, srv)
	if err != nil {
		return err
	}
	var upd proto.PendingUpdates
	cctx, cancel := context.WithTimeout(s.netCtx(ctx), 6*time.Minute)
	defer cancel()
	args := []string{}
	if refresh {
		args = append(args, "--refresh")
	}
	if err := s.pool.Call(cctx, ep, &upd, "updates", args...); err != nil {
		return s.hostErr(ctx, srv, err)
	}
	now := s.now()
	var state store.ServerState
	_ = s.db.WithContext(ctx).Select("security_seen").Where("server_id = ?", serverID).Take(&state).Error
	var prev map[string]time.Time
	_ = state.SecuritySeen.Decode(&prev)
	return s.db.WithContext(ctx).Model(&store.ServerState{}).Where("server_id = ?", serverID).
		Updates(map[string]any{
			"updates": store.MustJSON(upd), "updates_at": now,
			"security_seen": store.MustJSON(mergeSecuritySeen(prev, upd, now)),
		}).Error
}

// mergeSecuritySeen keeps the first time each pending security package was
// seen and drops packages that are no longer pending security updates.
func mergeSecuritySeen(prev map[string]time.Time, upd proto.PendingUpdates, now time.Time) map[string]time.Time {
	next := map[string]time.Time{}
	for _, p := range upd.Packages {
		if !p.Security {
			continue
		}
		if t, ok := prev[p.Name]; ok && !t.IsZero() {
			next[p.Name] = t
			continue
		}
		next[p.Name] = now
	}
	return next
}

// hostErr records a host key mismatch and converts helper errors into
// messages for the user.
func (s *Service) hostErr(ctx context.Context, srv store.Server, err error) error {
	var mismatch *remote.HostKeyMismatchError
	if errors.As(err, &mismatch) {
		s.recordMismatch(ctx, srv.ID, mismatch)
		return &UnavailableError{Message: srv.Name + ": " + mismatch.Error()}
	}
	var he *remote.HelperError
	if errors.As(err, &he) {
		switch he.Code {
		case proto.CodeBadRequest:
			return invalid("request", "%s", he.Message)
		case proto.CodeRefused, proto.CodeBusy:
			return &ConflictError{Message: srv.Name + ": " + he.Message}
		case proto.CodeIncompatible:
			return &ConflictError{Code: proto.CodeIncompatible, Message: srv.Name + ": " + he.Message}
		}
	}
	return &UnavailableError{Message: srv.Name + ": " + err.Error()}
}

// Rollup averages raw samples into hourly rows and applies retention.
func (s *Service) Rollup(ctx context.Context) {
	db := s.db.WithContext(ctx)
	err := db.Exec(`
		INSERT INTO metric_hourly (server_id, hour, cpu_percent, mem_percent, disk_percent, load1, samples)
		SELECT server_id, date_trunc('hour', at), avg(cpu_percent), avg(mem_percent), max(disk_percent), avg(load1), count(*)
		FROM metric_samples
		WHERE at < date_trunc('hour', now()) AND at >= date_trunc('hour', now()) - interval '48 hours'
		GROUP BY server_id, date_trunc('hour', at)
		ON CONFLICT (server_id, hour) DO UPDATE SET
			cpu_percent = excluded.cpu_percent, mem_percent = excluded.mem_percent,
			disk_percent = excluded.disk_percent, load1 = excluded.load1, samples = excluded.samples`).Error
	if err != nil {
		s.log.Error("roll up metrics", "err", err)
	}
	now := s.now()
	if err := db.Where("at < ?", now.Add(-s.rawRetention)).Delete(&store.MetricSample{}).Error; err != nil {
		s.log.Error("delete old samples", "err", err)
	}
	if err := db.Where("hour < ?", now.Add(-s.hourlyRetention)).Delete(&store.MetricHourly{}).Error; err != nil {
		s.log.Error("delete old hourly metrics", "err", err)
	}
}

// MetricPoint is one point on a chart.
type MetricPoint struct {
	At          time.Time
	CPUPercent  float64
	MemPercent  float64
	DiskPercent float64
	Load1       float64
}

// Metrics returns chart points for a server over the last `window`. Windows
// up to the raw retention (48 hours by default) use raw samples, longer
// windows use hourly averages. A window longer than the hourly retention
// returns only what is kept.
func (s *Service) Metrics(ctx context.Context, a Actor, serverID int64, window time.Duration) ([]MetricPoint, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	if _, _, err := s.loadVisibleServer(ctx, a, serverID); err != nil {
		return nil, err
	}
	since := s.now().Add(-window)
	points := []MetricPoint{}
	if window <= s.rawRetention {
		var rows []store.MetricSample
		if err := s.db.WithContext(ctx).Where("server_id = ? AND at >= ?", serverID, since).Order("at").Find(&rows).Error; err != nil {
			return nil, err
		}
		step := max(1, len(rows)/500) // at most ~500 points per chart
		for i := 0; i < len(rows); i += step {
			r := rows[i]
			points = append(points, MetricPoint{At: r.At, CPUPercent: r.CPUPercent, MemPercent: r.MemPercent, DiskPercent: r.DiskPercent, Load1: r.Load1})
		}
		return points, nil
	}
	var rows []store.MetricHourly
	if err := s.db.WithContext(ctx).Where("server_id = ? AND hour >= ?", serverID, since).Order("hour").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		points = append(points, MetricPoint{At: r.Hour, CPUPercent: round(r.CPUPercent), MemPercent: round(r.MemPercent), DiskPercent: round(r.DiskPercent), Load1: round(r.Load1)})
	}
	return points, nil
}
