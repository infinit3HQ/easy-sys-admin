package fleet

import (
	"context"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// PollDurationBuckets are the upper bounds, in seconds, of the poll
// duration histogram on /metrics.
var PollDurationBuckets = []float64{0.5, 1, 2, 5, 10, 30, 45}

// PollStats describes scheduled polling since the process started.
type PollStats struct {
	// The last finished cycle.
	LastCycleSeconds float64
	LastCycleServers int // ready servers at the start of the cycle
	LastCycleSkipped int // servers the cycle had no time to start
	// Scheduled polls by result. Deferred counts servers skipped by back-off.
	OK, Failed, Deferred uint64
	// Duration histogram of scheduled polls. Buckets are cumulative counts
	// for PollDurationBuckets.
	DurationBuckets []uint64
	DurationCount   uint64
	DurationSum     float64
}

// Stats are install-wide numbers for the /metrics endpoint.
type Stats struct {
	// ServersByStatus counts servers of every account by status. Servers
	// with no poll yet count as pending. Keys are the ServerStatus values.
	ServersByStatus map[ServerStatus]int64
	RunsRunning     int64
	SSHConnections  int
	NotifyQueue     int
	NotifyDropped   uint64
	Poll            PollStats
}

// statusesForStats are the statuses reported on /metrics, in order.
var statusesForStats = []ServerStatus{StatusOK, StatusWarning, StatusFailing, StatusUnreachable, StatusPending}

// StatusesForStats returns the fixed list of statuses Stats counts.
func StatusesForStats() []ServerStatus { return append([]ServerStatus(nil), statusesForStats...) }

// Stats returns install-wide counts for the unauthenticated /metrics
// endpoint. It takes no actor, so it must only ever return aggregate
// numbers: no names, hosts, emails, or account ids. The in-memory numbers
// are always filled in; the error reports a failed database read, in which
// case the server and run counts are zero.
func (s *Service) Stats(ctx context.Context) (Stats, error) {
	rt := s.rt()
	out := Stats{ServersByStatus: map[ServerStatus]int64{}}
	for _, st := range statusesForStats {
		out.ServersByStatus[st] = 0
	}
	if s.pool != nil {
		out.SSHConnections = s.pool.Len()
	}
	if q := rt.notifyQueue.Load(); q != nil {
		out.NotifyQueue = len(*q)
	}
	out.NotifyDropped = rt.notifyDropped.Load()
	rt.statsMu.Lock()
	out.Poll = rt.stats
	out.Poll.DurationBuckets = append([]uint64(nil), rt.stats.DurationBuckets...)
	rt.statsMu.Unlock()
	if len(out.Poll.DurationBuckets) != len(PollDurationBuckets) {
		out.Poll.DurationBuckets = make([]uint64, len(PollDurationBuckets))
	}

	var rows []struct {
		Status string
		N      int64
	}
	err := s.db.WithContext(ctx).Model(&store.Server{}).
		Select("COALESCE(server_states.status, 'pending') AS status, count(*) AS n").
		Joins("LEFT JOIN server_states ON server_states.server_id = servers.id").
		Group("COALESCE(server_states.status, 'pending')").Scan(&rows).Error
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		// Only known statuses, so the label set stays fixed.
		if _, ok := out.ServersByStatus[ServerStatus(r.Status)]; ok {
			out.ServersByStatus[ServerStatus(r.Status)] += r.N
		}
	}
	if err := s.db.WithContext(ctx).Model(&store.Run{}).Where("status = 'running'").Count(&out.RunsRunning).Error; err != nil {
		return out, err
	}
	return out, nil
}

// recordPoll adds one scheduled poll to the statistics.
func (s *Service) recordPoll(ok bool, d time.Duration) {
	rt := s.rt()
	sec := d.Seconds()
	rt.statsMu.Lock()
	defer rt.statsMu.Unlock()
	st := &rt.stats
	if ok {
		st.OK++
	} else {
		st.Failed++
	}
	if len(st.DurationBuckets) != len(PollDurationBuckets) {
		st.DurationBuckets = make([]uint64, len(PollDurationBuckets))
	}
	for i, le := range PollDurationBuckets {
		if sec <= le {
			st.DurationBuckets[i]++
		}
	}
	st.DurationCount++
	st.DurationSum += sec
}

// recordCycle stores the numbers of a finished poll cycle.
func (s *Service) recordCycle(d time.Duration, servers, skipped, deferred int) {
	rt := s.rt()
	rt.statsMu.Lock()
	defer rt.statsMu.Unlock()
	rt.stats.LastCycleSeconds = d.Seconds()
	rt.stats.LastCycleServers = servers
	rt.stats.LastCycleSkipped = skipped
	rt.stats.Deferred += uint64(deferred)
}
