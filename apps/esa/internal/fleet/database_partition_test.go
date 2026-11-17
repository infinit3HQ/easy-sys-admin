package fleet

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// databaseProxy cuts one control plane's database connections while the
// observer keeps a direct connection. It forwards PostgreSQL bytes without
// parsing or logging queries or credentials.
type databaseProxy struct {
	listener net.Listener
	target   string
	mu       sync.Mutex
	blocked  bool
	conns    map[net.Conn]struct{}
	workers  sync.WaitGroup
	rejected atomic.Int64
}

func newDatabaseProxy(t *testing.T, dsn string) (*databaseProxy, string) {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" {
		t.Skip("database partition tests need a PostgreSQL TCP URL")
	}
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &databaseProxy{listener: ln, target: net.JoinHostPort(u.Hostname(), port), conns: map[net.Conn]struct{}{}}
	t.Cleanup(func() {
		_ = ln.Close()
		p.partition(true)
		p.workers.Wait()
	})
	p.workers.Go(func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			if !p.track(client) {
				continue
			}
			p.workers.Go(func() { p.forward(client) })
		}
	})
	u.Host = ln.Addr().String()
	return p, u.String()
}

func (p *databaseProxy) track(c net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.blocked {
		p.rejected.Add(1)
		_ = c.Close()
		return false
	}
	p.conns[c] = struct{}{}
	return true
}

func (p *databaseProxy) close(c net.Conn) {
	_ = c.Close()
	p.mu.Lock()
	delete(p.conns, c)
	p.mu.Unlock()
}

func (p *databaseProxy) partition(blocked bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.blocked = blocked
	if blocked {
		for c := range p.conns {
			_ = c.Close()
		}
	}
}

func (p *databaseProxy) forward(client net.Conn) {
	defer p.close(client)
	upstream, err := net.DialTimeout("tcp", p.target, time.Second)
	if err != nil {
		return
	}
	if !p.track(upstream) {
		return
	}
	defer p.close(upstream)
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(upstream, client)
		_ = upstream.Close()
		close(done)
	}()
	_, _ = io.Copy(client, upstream)
	_ = client.Close()
	<-done
}

func TestDatabasePartitionBeforeRunOwnership(t *testing.T) {
	for _, state := range []string{"queued", "running"} {
		t.Run(state, func(t *testing.T) {
			s := newTestService(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			a := signIn(t, s, "partition-"+state)
			f := newFakeFleet(t, 0)
			f.noUpgradeUntilStart = state == "queued"
			acceptAccountKey(t, s, f, a.AccountID)
			ids := insertFakeServers(t, s, a.AccountID, []fakeHost{f.healthy()}, func(int) int { return 0 }, func(int) time.Time { return s.now() })
			run := store.Run{AccountID: a.AccountID, Kind: string(RunUpdate), Trigger: "manual", Status: "running", Options: store.JSON("{}"), StartedAt: s.now()}
			if err := s.db.Omit("Account", "Group").Create(&run).Error; err != nil {
				t.Fatal(err)
			}
			rs := store.RunServer{RunID: run.ID, ServerID: ids[0], Status: state, Detail: store.JSON("{}")}
			if err := s.db.Omit("Run", "Server").Create(&rs).Error; err != nil {
				t.Fatal(err)
			}
			proxy, dsn := newDatabaseProxy(t, os.Getenv("ESA_TEST_DATABASE_URL"))
			st, err := store.Open(ctx, dsn, s.log)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(st.Close)
			other := New(ctx, Deps{Store: st, Box: s.box, Pool: remote.NewPool(), PublicURL: s.publicURL, Logger: s.log, AllowPrivateNetworks: true})
			t.Cleanup(func() { cancel(); other.Wait(); other.pool.Close() })
			unlock, ok := s.tryRunLock(ctx, run.ID)
			if !ok {
				t.Fatal("the first control plane could not hold the run")
			}
			unlock = sync.OnceFunc(unlock)
			t.Cleanup(unlock)

			proxy.partition(true)
			// Neither an explicit worker nor the recovery loop may act
			// without a database connection on which to hold ownership.
			outageCtx, stop := context.WithTimeout(ctx, time.Second)
			other.execute(outageCtx, run.ID)
			stop()
			outageCtx, stop = context.WithTimeout(ctx, time.Second)
			other.ResumeRuns(outageCtx)
			stop()
			other.Wait()
			if proxy.rejected.Load() == 0 {
				t.Fatal("the test did not reject a database connection")
			}
			assertRun := func(wantRun, wantServer string) {
				t.Helper()
				var saved store.Run
				if err := s.db.Preload("Servers").First(&saved, run.ID).Error; err != nil {
					t.Fatal(err)
				}
				if saved.Status != wantRun || len(saved.Servers) != 1 || saved.Servers[0].Status != wantServer {
					t.Fatalf("run changed unexpectedly: %+v", saved)
				}
			}
			assertRun("running", state)
			if f.upgradeStarts.Load() != 0 {
				t.Fatal("a database outage started an upgrade")
			}

			proxy.partition(false)
			if err := st.Ping(ctx); err != nil {
				t.Fatalf("the database connection did not recover: %v", err)
			}
			other.ResumeRuns(ctx)
			other.Wait()
			assertRun("running", state)
			if f.upgradeStarts.Load() != 0 {
				t.Fatal("reconnecting took over a run still held by another control plane")
			}
			unlock()
			other.ResumeRuns(ctx)
			other.Wait()
			assertRun("succeeded", "succeeded")
			wantStarts := int64(0)
			if state == "queued" {
				wantStarts = 1
			}
			if got := f.upgradeStarts.Load(); got != wantStarts {
				t.Fatalf("upgrade-start calls after recovery = %d, want %d", got, wantStarts)
			}
		})
	}
}

// Lose the worker's database connections after it has acquired both locks
// and reached the helper's precheck. No install may follow an unsaved check.
func TestDatabasePartitionDuringUpgradePrecheck(t *testing.T) {
	s := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a := signIn(t, s, "partition-precheck")
	proxy, dsn := newDatabaseProxy(t, os.Getenv("ESA_TEST_DATABASE_URL"))
	st, err := store.Open(ctx, dsn, s.log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	worker := New(ctx, Deps{Store: st, Box: s.box, Pool: remote.NewPool(), PublicURL: s.publicURL, Logger: s.log, AllowPrivateNetworks: true})
	t.Cleanup(func() { cancel(); worker.Wait(); worker.pool.Close() })
	f := newFakeFleet(t, 0)
	f.noUpgradeUntilStart = true
	f.beforePrecheckReply = func() { proxy.partition(true) }
	acceptAccountKey(t, s, f, a.AccountID)
	ids := insertFakeServers(t, s, a.AccountID, []fakeHost{f.healthy()}, func(int) int { return 0 }, func(int) time.Time { return s.now() })
	run := store.Run{AccountID: a.AccountID, Kind: string(RunUpdate), Trigger: "manual", Status: "running", Options: store.JSON("{}"), StartedAt: s.now()}
	if err := s.db.Omit("Account", "Group").Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	rs := store.RunServer{RunID: run.ID, ServerID: ids[0], Status: "queued", Detail: store.JSON("{}")}
	if err := s.db.Omit("Run", "Server").Create(&rs).Error; err != nil {
		t.Fatal(err)
	}
	worker.execute(worker.netCtx(ctx), run.ID)
	if f.upgradeStarts.Load() != 0 {
		t.Fatal("an upgrade started after losing the database during precheck")
	}
	var saved store.RunServer
	if err := s.db.First(&saved, rs.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Status != "running" {
		t.Fatalf("the interrupted work must stay recoverable: %s", saved.Status)
	}
	// Recover using the independent control plane. The first worker never
	// sent an install, so recovery must send exactly one.
	s.allowPrivate = true
	t.Cleanup(s.pool.Close)
	s.ResumeRuns(ctx)
	s.Wait()
	if err := s.db.First(&saved, rs.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Status != "succeeded" || f.upgradeStarts.Load() != 1 {
		t.Fatalf("recovery: status=%s error=%s starts=%d", saved.Status, saved.Error, f.upgradeStarts.Load())
	}
}

func TestLostMaintenanceSessionCannotBeReplacedByReconnect(t *testing.T) {
	s := newTestService(t)
	proxy, dsn := newDatabaseProxy(t, os.Getenv("ESA_TEST_DATABASE_URL"))
	ctx, cancel := maintenanceContext(context.Background())
	defer cancel()
	st, err := store.Open(ctx, dsn, s.log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	worker := &Service{db: st.DB()}
	unlock, ok := worker.tryRunLock(ctx, 42)
	if !ok {
		t.Fatal("lock")
	}
	defer unlock()
	proxy.partition(true)
	proxy.partition(false)
	if err := st.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := ownershipFrom(ctx).check(ctx); !errors.Is(err, errMaintenanceOwnershipLost) {
		t.Fatalf("lost session was trusted: %v", err)
	}
	if !errors.Is(context.Cause(ctx), errMaintenanceOwnershipLost) {
		t.Fatal("work was not cancelled")
	}
}

func TestDatabasePartitionAfterInstallLeavesNextBatchQueued(t *testing.T) {
	s := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	a := signIn(t, s, "partition-after-install")
	proxy, dsn := newDatabaseProxy(t, os.Getenv("ESA_TEST_DATABASE_URL"))
	st, err := store.Open(ctx, dsn, s.log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	worker := New(ctx, Deps{Store: st, Box: s.box, Pool: remote.NewPool(), PublicURL: s.publicURL, Logger: s.log, AllowPrivateNetworks: true})
	t.Cleanup(func() { cancel(); worker.Wait(); worker.pool.Close() })
	f := newFakeFleet(t, 0)
	f.noUpgradeUntilStart = true
	f.afterUpgradeStart = func() { proxy.partition(true) }
	acceptAccountKey(t, s, f, a.AccountID)
	ids := insertFakeServers(t, s, a.AccountID, []fakeHost{f.healthy(), f.healthy()}, func(int) int { return 0 }, func(int) time.Time { return s.now() })
	run := store.Run{AccountID: a.AccountID, Kind: string(RunUpdate), Trigger: "manual", Status: "running", Options: store.JSON("{}"), StartedAt: s.now()}
	if err := s.db.Omit("Account", "Group").Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		rs := store.RunServer{RunID: run.ID, ServerID: id, Status: "queued", Detail: store.JSON("{}")}
		if err := s.db.Omit("Run", "Server").Create(&rs).Error; err != nil {
			t.Fatal(err)
		}
	}
	worker.execute(worker.netCtx(ctx), run.ID)
	var rows []store.RunServer
	if err := s.db.Where("run_id = ?", run.ID).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Status != "running" || rows[1].Status != "queued" || f.upgradeStarts.Load() != 1 {
		t.Fatalf("lost worker advanced: %+v starts=%d", rows, f.upgradeStarts.Load())
	}
	s.allowPrivate = true
	t.Cleanup(s.pool.Close)
	s.ResumeRuns(ctx)
	s.Wait()
	var finished store.Run
	if err := s.db.First(&finished, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if finished.Status != "succeeded" || f.upgradeStarts.Load() != 2 {
		t.Fatalf("recovery: status=%s starts=%d", finished.Status, f.upgradeStarts.Load())
	}
}
