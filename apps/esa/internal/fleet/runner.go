package fleet

import (
	"context"
	"errors"
	"fmt"
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

// execute runs a stored run to completion.
func (s *Service) execute(ctx context.Context, runID int64) {
	var run store.Run
	if err := s.db.WithContext(ctx).Preload("Servers").Preload("Servers.Server").Where("id = ?", runID).Take(&run).Error; err != nil {
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
		s.sequential(ctx, run.Servers, func(ctx context.Context, rs *store.RunServer) bool {
			return s.runUpdate(ctx, run, rs, opts)
		})
	case RunReboot:
		s.sequential(ctx, run.Servers, func(ctx context.Context, rs *store.RunServer) bool {
			return s.runReboot(ctx, run, rs)
		})
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

// sequential runs fn per server and stops at the first failure, so one bad
// package or a server that does not come back cannot take the whole group down.
func (s *Service) sequential(ctx context.Context, items []store.RunServer, fn func(context.Context, *store.RunServer) bool) {
	for i := range items {
		if ctx.Err() != nil {
			return
		}
		if !fn(ctx, &items[i]) {
			for j := i + 1; j < len(items); j++ {
				s.setRunServer(ctx, &items[j], "skipped", "stopped after "+items[i].Server.Name+" failed", nil, 0)
			}
			return
		}
	}
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
// records why) when the server cannot be worked on now.
func (s *Service) begin(ctx context.Context, rs *store.RunServer) (remote.Endpoint, func(), bool) {
	lock := s.serverLock(rs.ServerID)
	if !lock.TryLock() {
		s.setRunServer(ctx, rs, "skipped", "another run is working on this server", nil, 0)
		return remote.Endpoint{}, nil, false
	}
	var srv store.Server
	if err := s.db.WithContext(ctx).Where("id = ?", rs.ServerID).Take(&srv).Error; err != nil {
		lock.Unlock()
		s.setRunServer(ctx, rs, "failed", "server no longer exists", nil, 0)
		return remote.Endpoint{}, nil, false
	}
	if err := s.blockedByHostKey(ctx, srv); err != nil {
		lock.Unlock()
		s.setRunServer(ctx, rs, "failed", err.Error(), nil, 0)
		return remote.Endpoint{}, nil, false
	}
	ep, err := s.endpoint(ctx, srv)
	if err != nil {
		lock.Unlock()
		s.setRunServer(ctx, rs, "failed", err.Error(), nil, 0)
		return remote.Endpoint{}, nil, false
	}
	s.setRunServer(ctx, rs, "running", "", nil, 0)
	return ep, lock.Unlock, true
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
	s.setRunServer(ctx, rs, "succeeded", "", detail, 0)
	_ = s.RefreshUpdates(ctx, rs.ServerID, false)
	s.PollServer(ctx, rs.ServerID)
	return true
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

func (s *Service) finishRun(ctx context.Context, runID int64) {
	ctx = context.WithoutCancel(ctx)
	var run store.Run
	if err := s.db.WithContext(ctx).Preload("Servers").Preload("Servers.Server").Preload("Group").Where("id = ?", runID).Take(&run).Error; err != nil {
		return
	}
	var ok, failed, skipped int
	var freed int64
	for _, rs := range run.Servers {
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
	status := "succeeded"
	switch {
	case failed > 0 && ok == 0:
		status = "failed"
	case failed > 0:
		status = "partial"
	case ok == 0 && skipped > 0:
		status = "skipped"
	}
	summary := fmt.Sprintf("%d ok, %d failed, %d skipped", ok, failed, skipped)
	if RunKind(run.Kind) == RunClean {
		summary += ", freed " + humanBytes(freed)
	}
	now := s.now()
	s.db.WithContext(ctx).Model(&store.Run{}).Where("id = ?", run.ID).Updates(map[string]any{"status": status, "summary": summary, "finished_at": now})
	s.log.Info("run finished", "run_id", run.ID, "status", status, "summary", summary)
	if failed > 0 {
		run.Status, run.Summary = status, summary
		s.alertRunFailed(ctx, run)
	}
}

// ResumeRuns handles runs left "running" by a previous process. Upgrades
// keep running on the servers, so those are followed again; other runs
// are marked failed.
func (s *Service) ResumeRuns(ctx context.Context) {
	var runs []store.Run
	if err := s.db.WithContext(ctx).Preload("Servers").Preload("Servers.Server").Where("status = 'running'").Find(&runs).Error; err != nil {
		s.log.Error("load unfinished runs", "err", err)
		return
	}
	for _, run := range runs {
		if RunKind(run.Kind) != RunUpdate {
			for i := range run.Servers {
				if rs := &run.Servers[i]; rs.Status == "queued" || rs.Status == "running" {
					s.setRunServer(ctx, rs, "failed", "interrupted: the control plane restarted", nil, 0)
				}
			}
			s.finishRun(ctx, run.ID)
			continue
		}
		run := run
		s.background(func(ctx context.Context) {
			var opts RunOptions
			_ = run.Options.Decode(&opts)
			for i := range run.Servers {
				rs := &run.Servers[i]
				switch rs.Status {
				case "running":
					ep, err := s.endpoint(ctx, rs.Server)
					if err != nil || !s.followUpgrade(ctx, run, rs, ep, opts) {
						if err != nil {
							s.setRunServer(ctx, rs, "failed", err.Error(), nil, 0)
						}
						s.skipRest(ctx, run.Servers[i+1:], rs.Server.Name)
						s.finishRun(ctx, run.ID)
						return
					}
				case "queued":
					if !s.runUpdate(ctx, run, rs, opts) {
						s.skipRest(ctx, run.Servers[i+1:], rs.Server.Name)
						s.finishRun(ctx, run.ID)
						return
					}
				}
			}
			s.finishRun(ctx, run.ID)
		})
	}
}

func (s *Service) skipRest(ctx context.Context, rest []store.RunServer, after string) {
	for j := range rest {
		if rest[j].Status == "queued" {
			s.setRunServer(ctx, &rest[j], "skipped", "stopped after "+after+" failed", nil, 0)
		}
	}
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
