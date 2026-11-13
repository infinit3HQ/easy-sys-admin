package fleet

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

type gormDB = gorm.DB

const (
	cleanParallel     = 4
	cleanTimeout      = 10 * time.Minute
	upgradeTimeout    = 2 * time.Hour
	upgradePollEvery  = 10 * time.Second
	rebootWaitTimeout = 10 * time.Minute
)

// serverLock returns the mutex that serialises maintenance on one server.
func (s *Service) serverLock(id int64) *sync.Mutex {
	m, _ := s.serverLocks.LoadOrStore(id, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// execute runs a stored run to completion. The run lock is held until it
// returns, so a second control plane does not resume the same run.
func (s *Service) execute(ctx context.Context, runID int64) {
	unlock, ok := s.tryRunLock(ctx, runID)
	if !ok {
		s.log.Info("run left with the process that already holds it", "run_id", runID)
		return
	}
	defer unlock()
	var run store.Run
	if err := s.db.WithContext(ctx).Preload("Servers", byID).Preload("Servers.Server").Where("id = ?", runID).Take(&run).Error; err != nil {
		s.log.Error("load run", "run_id", runID, "err", err)
		return
	}
	var opts RunOptions
	_ = run.Options.Decode(&opts)
	log := s.log.With("run_id", run.ID, "kind", run.Kind)
	log.Info("run started", "servers", len(run.Servers))

	switch RunKind(run.Kind) {
	case RunMeasure, RunClean:
		s.parallel(ctx, run.Servers, cleanParallel, func(ctx context.Context, rs *store.RunServer) {
			s.runClean(ctx, rs, RunKind(run.Kind) == RunClean, opts)
		})
	case RunUpdate:
		s.batched(ctx, run.Servers, opts, nil, func(ctx context.Context, rs *store.RunServer) bool {
			return s.runUpdate(ctx, run, rs, opts)
		})
	case RunReboot:
		s.batched(ctx, run.Servers, opts, nil, func(ctx context.Context, rs *store.RunServer) bool {
			return s.runReboot(ctx, run, rs)
		})
	}
	if ctx.Err() != nil {
		// The process is stopping. Leave the run "running" so the next
		// start resumes it, instead of finishing it with work outstanding.
		log.Info("run left for the next start", "reason", ctx.Err())
		return
	}
	s.finishRun(ctx, run.ID)
}

func (s *Service) parallel(ctx context.Context, items []store.RunServer, n int, fn func(context.Context, *store.RunServer)) {
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for i := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(rs *store.RunServer) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(ctx, rs)
		}(&items[i])
	}
	wg.Wait()
}

// byID orders a run's servers as the run listed them: the group's order.
func byID(db *gormDB) *gormDB { return db.Order("id") }

// batched runs fn on the queued servers in batches of the run's batch size,
// in order. The servers of one batch run at the same time, and the next batch
// starts only when the whole batch is done. Once the failed servers reach the
// run's limit, the rest are skipped, so one bad package or a server that does
// not come back cannot take the whole group down. failed holds the indexes of
// servers that already failed in this run.
func (s *Service) batched(ctx context.Context, items []store.RunServer, o RunOptions, failed []int, fn func(context.Context, *store.RunServer) bool) {
	size, maxFailures := o.batching()
	queued := make([]int, 0, len(items))
	for i := range items {
		if items[i].Status == "queued" {
			queued = append(queued, i)
		}
	}
	work := func(ctx context.Context, i int) bool { return fn(ctx, &items[i]) }
	canary, rest := partitionCanaries(items, queued, o.CanaryServerIDs)
	// After a restart the canaries may already be done while the rest still
	// wait. The canary rules and the watch still apply then.
	allCanary := canaryIndexes(items, o.CanaryServerIDs)
	if len(allCanary) > 0 && !restStarted(items, allCanary) {
		batches := planBatches(items, canary, len(canary))
		var started int
		failed, started = runBatches(ctx, batches, maxFailures, failed, work)
		reason := ""
		if !canaryFailed(items, allCanary, failed) && canaryDidNotFinish(items, allCanary) {
			reason = "stopped after a canary did not finish"
		}
		if reason != "" || canaryFailed(items, allCanary, failed) || stopRun(len(failed), maxFailures) {
			s.skipRest(ctx, items, append(unstarted(batches, started), rest...), failed, allCanary, reason)
			return
		}
		if reason := s.watchCanaries(ctx, items, allCanary, o); reason != "" {
			s.skipRest(ctx, items, rest, failed, allCanary, "stopped after the watch: "+reason)
			return
		}
		if o.WaitAfterCanaries && len(rest) > 0 {
			ok, why := s.awaitGate(ctx, items[0].RunID)
			if !ok {
				if why != "" {
					s.skipRest(ctx, items, rest, failed, allCanary, why)
				}
				return
			}
		}
	}
	batches := planBatches(items, rest, size)
	var started int
	failed, started = runBatches(ctx, batches, maxFailures, failed, work)
	if !stopRun(len(failed), maxFailures) {
		return
	}
	s.skipRest(ctx, items, unstarted(batches, started), failed, nil, "")
}

func unstarted(batches [][]int, started int) []int {
	var out []int
	for _, batch := range batches[started:] {
		out = append(out, batch...)
	}
	return out
}

// canaryIndexes returns the indexes of every canary in the run, whatever
// its status.
func canaryIndexes(items []store.RunServer, canaryIDs []int64) []int {
	var out []int
	for i := range items {
		if slices.Contains(canaryIDs, items[i].ServerID) {
			out = append(out, i)
		}
	}
	return out
}

// restStarted is true once a server that is not a canary left the queue,
// so the canary phase is over.
func restStarted(items []store.RunServer, canary []int) bool {
	for i := range items {
		if !slices.Contains(canary, i) && items[i].Status != "queued" {
			return true
		}
	}
	return false
}

// canaryDidNotFinish is true when a canary was skipped or never reached
// succeeded. A skip is not a pass, and the other servers must not start.
func canaryDidNotFinish(items []store.RunServer, canary []int) bool {
	for _, i := range canary {
		if items[i].Status != "succeeded" {
			return true
		}
	}
	return false
}

func canaryFailed(items []store.RunServer, canary, failed []int) bool {
	isCanary := map[int]bool{}
	for _, i := range canary {
		isCanary[i] = true
	}
	for _, i := range failed {
		if isCanary[i] && items[i].Status == "failed" {
			return true
		}
	}
	return false
}

func (s *Service) skipRest(ctx context.Context, items []store.RunServer, idx, failed, canary []int, reason string) {
	msg := reason
	if msg == "" {
		msg = stopMessage(items, failed)
		isCanary := map[int]bool{}
		for _, i := range canary {
			isCanary[i] = true
		}
		for _, i := range failed {
			if isCanary[i] && items[i].Status == "failed" {
				msg = "stopped after canary " + items[i].Server.Name + " failed"
				break
			}
		}
	}
	for _, i := range idx {
		if items[i].Status == "queued" {
			s.setRunServer(ctx, &items[i], "skipped", msg, nil, 0)
		}
	}
}

// planBatches splits the servers at idx, in order, into batches of at most
// size. The server that hosts the control plane always gets a batch of its
// own, so its upgrade never runs next to another server's and a restart of
// the control plane interrupts no one else.
func planBatches(items []store.RunServer, idx []int, size int) [][]int {
	size = max(size, 1)
	var out [][]int
	var cur []int
	for _, i := range idx {
		if items[i].Server.HostsControlPlane {
			if len(cur) > 0 {
				out, cur = append(out, cur), nil
			}
			out = append(out, []int{i})
			continue
		}
		if cur = append(cur, i); len(cur) == size {
			out, cur = append(out, cur), nil
		}
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// runBatches runs work on each batch in turn and returns the failed indexes
// (after the ones passed in) and how many batches it started. It starts no
// batch once the failures reach maxFailures or ctx ends.
func runBatches(ctx context.Context, batches [][]int, maxFailures int, failed []int, work func(context.Context, int) bool) ([]int, int) {
	for b, batch := range batches {
		if ctx.Err() != nil || stopRun(len(failed), maxFailures) {
			return failed, b
		}
		failed = append(failed, runBatch(ctx, batch, work)...)
	}
	return failed, len(batches)
}

// runBatch runs work on every index at the same time, waits for all of
// them, and returns the indexes that failed, in batch order.
func runBatch(ctx context.Context, batch []int, work func(context.Context, int) bool) []int {
	ok := make([]bool, len(batch))
	var wg sync.WaitGroup
	for j, i := range batch {
		wg.Go(func() { ok[j] = work(ctx, i) })
	}
	wg.Wait()
	var failed []int
	for j, i := range batch {
		if !ok[j] {
			failed = append(failed, i)
		}
	}
	return failed
}

// stopRun reports whether a run with this many failed servers starts no
// more batches.
func stopRun(failed, maxFailures int) bool {
	return failed >= max(maxFailures, 1)
}

// stopMessage says why the rest of a run was skipped.
func stopMessage(items []store.RunServer, failed []int) string {
	if len(failed) == 1 {
		return "stopped after " + items[failed[0]].Server.Name + " failed"
	}
	return fmt.Sprintf("stopped after %d failed servers", len(failed))
}

func (s *Service) setRunServer(ctx context.Context, rs *store.RunServer, status, errMsg string, detail any, freed int64) {
	now := s.now()
	changes := map[string]any{"status": status, "error": errMsg, "bytes_freed": freed}
	if detail != nil {
		changes["detail"] = store.MustJSON(detail)
	}
	switch status {
	case "running":
		changes["started_at"] = now
	default:
		changes["finished_at"] = now
		if rs.StartedAt == nil {
			changes["started_at"] = now
		}
	}
	if status == "running" {
		rs.StartedAt = &now
	}
	rs.Status = status
	if err := s.db.WithContext(context.WithoutCancel(ctx)).Model(&store.RunServer{}).Where("id = ?", rs.ID).Updates(changes).Error; err != nil {
		s.log.Error("update run server", "id", rs.ID, "err", err)
	}
}

// begin locks the server and resolves its endpoint. It returns false (and
// records why) when the server cannot be worked on now. The database lock
// is held on one connection so a second control plane process skips the
// same server instead of starting a second run.
func (s *Service) begin(ctx context.Context, rs *store.RunServer) (remote.Endpoint, func(), bool) {
	unlock, busy := s.lockServer(ctx, rs.ServerID)
	if unlock == nil {
		s.setRunServer(ctx, rs, "skipped", busy, nil, 0)
		return remote.Endpoint{}, nil, false
	}
	var srv store.Server
	if err := s.db.WithContext(ctx).Where("id = ?", rs.ServerID).Take(&srv).Error; err != nil {
		unlock()
		s.setRunServer(ctx, rs, "failed", "server no longer exists", nil, 0)
		return remote.Endpoint{}, nil, false
	}
	if err := s.blockedByHostKey(ctx, srv); err != nil {
		unlock()
		s.setRunServer(ctx, rs, "failed", err.Error(), nil, 0)
		return remote.Endpoint{}, nil, false
	}
	ep, err := s.endpoint(ctx, srv)
	if err != nil {
		unlock()
		s.setRunServer(ctx, rs, "failed", err.Error(), nil, 0)
		return remote.Endpoint{}, nil, false
	}
	s.setRunServer(ctx, rs, "running", "", nil, 0)
	return ep, unlock, true
}

// lockServer takes the in-process and the database lock for one server.
// It returns nil and the reason when another run holds either.
func (s *Service) lockServer(ctx context.Context, id int64) (func(), string) {
	lock := s.serverLock(id)
	if !lock.TryLock() {
		return nil, "another run is working on this server"
	}
	unlockDB, ok := s.tryServerLock(ctx, id)
	if !ok {
		lock.Unlock()
		return nil, "another control plane is working on this server"
	}
	return func() {
		unlockDB()
		lock.Unlock()
	}, ""
}

// runLockNamespace keeps a run lock from colliding with a server lock.
// Server locks use the server id. Run locks use this bit plus the run id.
const runLockNamespace int64 = 1 << 62

// tryServerLock takes a PostgreSQL advisory lock for this server and holds
// the connection until unlock. The in-process mutex does not cover a second
// process.
func (s *Service) tryServerLock(ctx context.Context, id int64) (func(), bool) {
	return s.tryAdvisoryLock(ctx, id)
}

// tryRunLock is held for the whole run. A second process leaves the run
// alone instead of marking its servers failed.
func (s *Service) tryRunLock(ctx context.Context, runID int64) (func(), bool) {
	return s.tryAdvisoryLock(ctx, runLockNamespace+runID)
}

func (s *Service) tryAdvisoryLock(ctx context.Context, key int64) (func(), bool) {
	sqlDB, err := s.db.DB()
	if err != nil {
		return nil, false
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, false
	}
	var got bool
	err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&got)
	if err != nil || !got {
		_ = conn.Close()
		return nil, false
	}
	return func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", key)
		_ = conn.Close()
	}, true
}

func (s *Service) failHost(ctx context.Context, rs *store.RunServer, err error) {
	var mismatch *remote.HostKeyMismatchError
	if errors.As(err, &mismatch) {
		s.recordMismatch(ctx, rs.ServerID, mismatch)
	}
	s.setRunServer(ctx, rs, "failed", err.Error(), nil, 0)
}

// ---- Clean and measure ---------------------------------------------------------

func (s *Service) runClean(ctx context.Context, rs *store.RunServer, apply bool, o RunOptions) {
	ep, unlock, ok := s.begin(ctx, rs)
	if !ok {
		return
	}
	defer unlock()

	args := []string{"--measure", "--journal-size", o.JournalSize}
	if apply {
		var only []string
		if o.BuildCache {
			only = append(only, proto.CleanBuildCache)
		}
		if o.VSCode {
			only = append(only, proto.CleanVSCode)
		}
		if o.Journal {
			only = append(only, proto.CleanJournal)
		}
		args = []string{"--apply", "--only", strings.Join(only, ","), "--journal-size", o.JournalSize}
	}
	var rep proto.CleanReport
	cctx, cancel := context.WithTimeout(ctx, cleanTimeout)
	err := s.pool.Call(cctx, ep, &rep, "clean", args...)
	cancel()
	if err != nil {
		s.failHost(ctx, rs, err)
		return
	}
	var freed int64
	var failed []string
	skipped := 0
	for _, it := range rep.Items {
		freed += it.Freed
		switch it.Status {
		case "failed":
			failed = append(failed, it.ID+": "+it.Error)
		case "skipped":
			skipped++
		}
	}
	status := "succeeded"
	switch {
	case len(failed) > 0:
		status = "failed"
	case apply && skipped == len(rep.Items):
		status = "skipped"
	}
	s.setRunServer(ctx, rs, status, strings.Join(failed, "; "), rep, freed)
	if apply {
		s.PollServer(ctx, rs.ServerID)
	}
}

// ---- Updates ---------------------------------------------------------------------

func (s *Service) runUpdate(ctx context.Context, run store.Run, rs *store.RunServer, o RunOptions) bool {
	ep, unlock, ok := s.begin(ctx, rs)
	if !ok {
		return rs.Status == "skipped" && strings.Contains(rs.Error, "another run")
	}
	defer unlock()

	var pc proto.Precheck
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	err := s.pool.Call(cctx, ep, &pc, "upgrade-precheck")
	cancel()
	if err != nil {
		s.failHost(ctx, rs, err)
		return false
	}
	if !pc.OK {
		s.setRunServer(ctx, rs, "failed", "pre-check: "+strings.Join(pc.Problems, "; "), map[string]any{"precheck": pc}, 0)
		return false
	}

	args := []string{"--run-id", strconv.FormatInt(run.ID, 10), "--mode", string(o.UpdateMode)}
	if len(o.Hold) > 0 {
		args = append(args, "--hold", strings.Join(o.Hold, ","))
	}
	if o.Approved != nil {
		allow := proto.AllowNone
		if len(*o.Approved) > 0 {
			allow = strings.Join(*o.Approved, ",")
		}
		args = append(args, "--allow", allow)
	}
	before, err := s.readHealth(ctx, ep)
	if err != nil {
		s.setRunServer(ctx, rs, "failed", "could not read status before the upgrade: "+err.Error(), nil, 0)
		return false
	}
	pre, block := s.runHealthChecks(ctx, rs.Server, ep, beforeChecks(o.Checks, o.Override), "before")
	markOverridden(pre, o.Override)
	saved := map[string]any{"healthBefore": before}
	if len(pre) > 0 {
		saved["checks"] = pre
	}
	if block != "" {
		s.setRunServer(ctx, rs, "failed", "check before upgrade: "+block, saved, 0)
		return false
	}
	rs.Detail = store.MustJSON(saved)
	s.db.WithContext(ctx).Model(rs).Update("detail", rs.Detail)

	cctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
	err = s.pool.Call(cctx, ep, nil, "upgrade-start", args...)
	cancel()
	if err != nil {
		s.failHost(ctx, rs, err)
		return false
	}
	return s.followUpgrade(ctx, run, rs, ep, o)
}

// followUpgrade polls the detached upgrade until it finishes. Connection
// errors are retried, because the upgrade itself does not depend on SSH.
func (s *Service) followUpgrade(ctx context.Context, run store.Run, rs *store.RunServer, ep remote.Endpoint, o RunOptions) bool {
	deadline := time.Now().Add(upgradeTimeout)
	var res proto.UpgradeResult
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(upgradePollEvery):
		}
		cctx, cancel := context.WithTimeout(ctx, time.Minute)
		lastErr = s.pool.Call(cctx, ep, &res, "upgrade-status", "--run-id", strconv.FormatInt(run.ID, 10))
		cancel()
		if lastErr != nil {
			var mismatch *remote.HostKeyMismatchError
			if errors.As(lastErr, &mismatch) {
				s.failHost(ctx, rs, lastErr)
				return false
			}
			s.log.Warn("upgrade status (will retry)", "run_id", run.ID, "server_id", rs.ServerID, "err", lastErr)
			continue
		}
		if res.State != "running" {
			break
		}
	}
	if res.State == "running" || res.State == "" {
		msg := "upgrade did not finish within " + upgradeTimeout.String()
		if lastErr != nil {
			msg += "; last error: " + lastErr.Error()
		}
		s.setRunServer(ctx, rs, "failed", msg, res, 0)
		return false
	}
	if res.State == "failed" {
		s.setRunServer(ctx, rs, "failed", res.Error, res, 0)
		_ = s.RefreshUpdates(ctx, rs.ServerID, false)
		return false
	}
	detail := map[string]any{"upgrade": res}
	if res.RebootRequired && o.RebootIfRequired {
		var srv store.Server
		s.db.WithContext(ctx).Where("id = ?", rs.ServerID).Take(&srv)
		if srv.HostsControlPlane {
			detail["reboot"] = "skipped: this server hosts the control plane"
		} else if err := s.rebootAndWait(ctx, ep, rs.ServerID); err != nil {
			s.setRunServer(ctx, rs, "failed", "upgrade succeeded, reboot failed: "+err.Error(), detail, 0)
			return false
		} else {
			detail["reboot"] = "rebooted"
		}
	}
	if msg := s.healthRegressed(ctx, ep, healthBefore(rs)); msg != "" {
		detail["health"] = msg
		detail["checks"] = savedChecks(rs)
		s.setRunServer(ctx, rs, "failed", "upgrade finished, "+msg, detail, 0)
		_ = s.RefreshUpdates(ctx, rs.ServerID, false)
		s.PollServer(ctx, rs.ServerID)
		return false
	}
	post, block := s.runHealthChecks(ctx, rs.Server, ep, o.Checks, "after")
	if all := append(savedChecks(rs), post...); len(all) > 0 {
		detail["checks"] = all
	}
	if block != "" {
		s.setRunServer(ctx, rs, "failed", "check after upgrade: "+block, detail, 0)
		_ = s.RefreshUpdates(ctx, rs.ServerID, false)
		s.PollServer(ctx, rs.ServerID)
		return false
	}
	s.setRunServer(ctx, rs, "succeeded", "", detail, 0)
	_ = s.RefreshUpdates(ctx, rs.ServerID, false)
	s.PollServer(ctx, rs.ServerID)
	return true
}

// serviceHealth is the failed units and bad containers seen at one moment.
type serviceHealth struct {
	FailedUnits   []string `json:"failedUnits"`
	BadContainers int      `json:"badContainers"`
}

func (s *Service) readHealth(ctx context.Context, ep remote.Endpoint) (serviceHealth, error) {
	var snap proto.Status
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if err := s.pool.Call(cctx, ep, &snap, "status"); err != nil {
		return serviceHealth{}, err
	}
	h := serviceHealth{FailedUnits: append([]string(nil), snap.FailedUnits...)}
	slices.Sort(h.FailedUnits)
	if snap.Docker != nil {
		h.BadContainers = snap.Docker.Unhealthy + snap.Docker.Restarting
	}
	return h, nil
}

func savedChecks(rs *store.RunServer) []CheckResult {
	var saved struct {
		Checks []CheckResult `json:"checks"`
	}
	_ = rs.Detail.Decode(&saved)
	return saved.Checks
}

func healthBefore(rs *store.RunServer) serviceHealth {
	var saved struct {
		HealthBefore serviceHealth `json:"healthBefore"`
	}
	_ = rs.Detail.Decode(&saved)
	return saved.HealthBefore
}

// healthRegressed reports a failed unit or a rise in unhealthy containers
// that was not already there. A status read that fails is itself a stop.
func (s *Service) healthRegressed(ctx context.Context, ep remote.Endpoint, before serviceHealth) string {
	after, err := s.readHealth(ctx, ep)
	if err != nil {
		return "status could not be read after the upgrade: " + err.Error()
	}
	return healthWorse(before, after)
}

func healthWorse(before, after serviceHealth) string {
	had := map[string]bool{}
	for _, u := range before.FailedUnits {
		had[u] = true
	}
	var fresh []string
	for _, u := range after.FailedUnits {
		if !had[u] {
			fresh = append(fresh, u)
		}
	}
	if len(fresh) > 0 {
		return "new failed units: " + strings.Join(fresh, ", ")
	}
	if after.BadContainers > before.BadContainers {
		return fmt.Sprintf("unhealthy or restarting containers went from %d to %d", before.BadContainers, after.BadContainers)
	}
	return ""
}

// ---- Reboots ---------------------------------------------------------------------

func (s *Service) runReboot(ctx context.Context, run store.Run, rs *store.RunServer) bool {
	if run.Trigger == string(TriggerSchedule) && rs.Server.HostsControlPlane {
		s.setRunServer(ctx, rs, "skipped", "this server hosts the control plane and is never rebooted by a schedule", nil, 0)
		return true
	}
	ep, unlock, ok := s.begin(ctx, rs)
	if !ok {
		return false
	}
	defer unlock()
	if err := s.rebootAndWait(ctx, ep, rs.ServerID); err != nil {
		s.setRunServer(ctx, rs, "failed", err.Error(), nil, 0)
		return false
	}
	s.setRunServer(ctx, rs, "succeeded", "", map[string]any{"reboot": "rebooted"}, 0)
	s.PollServer(ctx, rs.ServerID)
	return true
}

// rebootAndWait reboots, waits for a new boot time, then checks that every
// container with a restart policy is running again.
func (s *Service) rebootAndWait(ctx context.Context, ep remote.Endpoint, serverID int64) error {
	var before proto.Status
	cctx, cancel := context.WithTimeout(ctx, pollTimeout)
	err := s.pool.Call(cctx, ep, &before, "status")
	cancel()
	if err != nil {
		return fmt.Errorf("read status before reboot: %w", err)
	}
	var running []proto.Container
	cctx, cancel = context.WithTimeout(ctx, time.Minute)
	_ = s.pool.Call(cctx, ep, &running, "containers")
	cancel()

	cctx, cancel = context.WithTimeout(ctx, time.Minute)
	err = s.pool.Call(cctx, ep, nil, "reboot")
	cancel()
	if err != nil {
		return fmt.Errorf("schedule reboot: %w", err)
	}
	s.pool.Forget(serverID)

	deadline := time.Now().Add(rebootWaitTimeout)
	var after proto.Status
	for {
		if time.Now().After(deadline) {
			return errors.New("server did not come back within " + rebootWaitTimeout.String())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(15 * time.Second):
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := s.pool.Call(cctx, ep, &after, "status")
		cancel()
		if err != nil {
			s.pool.Forget(serverID)
			var mismatch *remote.HostKeyMismatchError
			if errors.As(err, &mismatch) {
				s.recordMismatch(ctx, serverID, mismatch)
				return err
			}
			continue
		}
		if after.BootTime.After(before.BootTime.Add(30 * time.Second)) {
			break
		}
	}

	// Give restart policies a moment, then compare.
	time.Sleep(20 * time.Second)
	var now []proto.Container
	cctx, cancel = context.WithTimeout(ctx, time.Minute)
	err = s.pool.Call(cctx, ep, &now, "containers")
	cancel()
	if err != nil {
		return fmt.Errorf("list containers after reboot: %w", err)
	}
	state := map[string]string{}
	for _, c := range now {
		state[c.Name] = c.State
	}
	var down []string
	for _, c := range running {
		if c.State == "running" && c.RestartPolicy != "" && c.RestartPolicy != "no" && state[c.Name] != "running" {
			down = append(down, c.Name)
		}
	}
	if len(down) > 0 {
		return errors.New("back up, but these containers did not restart: " + strings.Join(down, ", "))
	}
	return nil
}

// ---- Finishing -----------------------------------------------------------------------

// runTally counts a run's server results and builds the run's summary line:
// "2 ok, 1 failed, 0 skipped", plus the bytes freed for cleanups.
func runTally(kind RunKind, servers []store.RunServer) (ok, failed, skipped int, summary string) {
	var freed int64
	for _, rs := range servers {
		freed += rs.BytesFreed
		switch rs.Status {
		case "succeeded":
			ok++
		case "failed":
			failed++
		default:
			skipped++
		}
	}
	summary = fmt.Sprintf("%d ok, %d failed, %d skipped", ok, failed, skipped)
	if kind == RunClean {
		summary += ", freed " + humanBytes(freed)
	}
	return ok, failed, skipped, summary
}

func (s *Service) finishRun(ctx context.Context, runID int64) {
	ctx = context.WithoutCancel(ctx)
	var run store.Run
	if err := s.db.WithContext(ctx).Preload("Servers").Preload("Servers.Server").Preload("Group").Where("id = ?", runID).Take(&run).Error; err != nil {
		return
	}
	ok, failed, _, summary := runTally(RunKind(run.Kind), run.Servers)
	aborted := runAborted(run.Servers)
	status := runOutcome(ok, failed, aborted)
	now := s.now()
	s.db.WithContext(ctx).Model(&store.Run{}).Where("id = ?", run.ID).Updates(map[string]any{"status": status, "summary": summary, "finished_at": now})
	// A plan that started this run ends with the run's status.
	s.db.WithContext(ctx).Model(&store.FixPlan{}).Where("run_id = ? AND status = ?", run.ID, "executing").
		Updates(map[string]any{"status": status, "updated_at": now})
	s.log.Info("run finished", "run_id", run.ID, "status", status, "summary", summary)
	if failed > 0 || aborted {
		run.Status, run.Summary = status, summary
		s.alertRunFailed(ctx, run)
	}
}

// runAborted reports whether the rest of a run was stopped by a canary or a
// watch. Those servers are skipped, which is not a successful run.
func runAborted(servers []store.RunServer) bool {
	for _, rs := range servers {
		if rs.Status == "skipped" && strings.HasPrefix(rs.Error, "stopped") {
			return true
		}
	}
	return false
}

// runOutcome is the run status. A stop after a canary or a watch is failed
// when nothing succeeded, and partial when a canary did.
func runOutcome(ok, failed int, aborted bool) string {
	switch {
	case (failed > 0 || aborted) && ok == 0:
		return "failed"
	case failed > 0 || aborted:
		return "partial"
	case ok == 0:
		return "skipped"
	default:
		return "succeeded"
	}
}

// ResumeRuns handles runs left "running" when their worker is gone. It runs
// at startup and every minute. Upgrades keep running on the servers, so
// those are followed again and the run goes on with its next batches; other
// runs are marked failed. A run whose worker still holds the lock is left
// alone.
func (s *Service) ResumeRuns(ctx context.Context) {
	var runs []store.Run
	if err := s.db.WithContext(ctx).Preload("Servers", byID).Preload("Servers.Server").Where("status = 'running'").Find(&runs).Error; err != nil {
		s.log.Error("load unfinished runs", "err", err)
		return
	}
	for _, run := range runs {
		unlock, ok := s.tryRunLock(ctx, run.ID)
		if !ok {
			s.log.Info("unfinished run is still held", "run_id", run.ID)
			continue
		}
		if RunKind(run.Kind) != RunUpdate {
			for i := range run.Servers {
				if rs := &run.Servers[i]; rs.Status == "queued" || rs.Status == "running" {
					s.setRunServer(ctx, rs, "failed", "interrupted: the worker that started this run is gone", nil, 0)
				}
			}
			s.finishRun(ctx, run.ID)
			unlock()
			continue
		}
		run := run
		s.background(func(ctx context.Context) {
			defer unlock()
			var opts RunOptions
			_ = run.Options.Decode(&opts)
			failed, running := resumeState(run.Servers)
			// Every server still "running" belongs to the batch that was in
			// flight. Their upgrades went on without us, so follow all of them
			// before the next batch starts.
			failed = append(failed, runBatch(ctx, running, func(ctx context.Context, i int) bool {
				rs := &run.Servers[i]
				unlockServer, busy := s.lockServer(ctx, rs.ServerID)
				if unlockServer == nil {
					s.setRunServer(ctx, rs, "failed", busy+"; the upgrade may still be running there", nil, 0)
					return false
				}
				defer unlockServer()
				ep, err := s.endpoint(ctx, rs.Server)
				if err != nil {
					s.setRunServer(ctx, rs, "failed", err.Error(), nil, 0)
					return false
				}
				return s.followUpgrade(ctx, run, rs, ep, opts)
			})...)
			s.batched(ctx, run.Servers, opts, failed, func(ctx context.Context, rs *store.RunServer) bool {
				return s.runUpdate(ctx, run, rs, opts)
			})
			if ctx.Err() != nil {
				return
			}
			s.finishRun(ctx, run.ID)
		})
	}
}

// resumeState returns the indexes of a run's servers that already failed and
// of those still running (the batch in flight when the process stopped).
func resumeState(servers []store.RunServer) (failed, running []int) {
	for i := range servers {
		switch servers[i].Status {
		case "failed":
			failed = append(failed, i)
		case "running":
			running = append(running, i)
		}
	}
	return failed, running
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
