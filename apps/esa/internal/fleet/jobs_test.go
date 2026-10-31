package fleet

import (
	"context"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func addTestServer(t *testing.T, s *Service, a Actor, groupID *int64, name string) store.Server {
	t.Helper()
	srv := store.Server{AccountID: a.AccountID, GroupID: groupID, Name: name, Host: "192.0.2.1", Port: 22, Username: "esa",
		Enrollment: "pending", ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	return srv
}

func countRuns(t *testing.T, s *Service, groupID int64, status string) int64 {
	t.Helper()
	var n int64
	q := s.db.Model(&store.Run{}).Where("group_id = ?", groupID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	q.Count(&n)
	return n
}

// A due slot starts exactly one run, saving the form never starts one, and a
// slot missed by more than an hour is recorded instead of run late.
func TestSchedulerTick(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "sched")
	all := []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

	recent := time.Now().UTC().Add(-10 * time.Minute).Format("15:04")
	g, err := s.CreateGroup(ctx, a, GroupInput{Name: "due", Timezone: "UTC",
		Clean:   CleanPolicy{Enabled: true, Days: all, Time: recent, BuildCache: true, JournalSize: "200M"},
		Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00"}})
	if err != nil {
		t.Fatal(err)
	}
	addTestServer(t, s, a, &g.ID, "due-1")

	s.Tick(ctx)
	if n := countRuns(t, s, g.ID, ""); n != 0 {
		t.Fatalf("saving the group must not start a run for a slot that already passed, got %d", n)
	}

	// Pretend the slot was never claimed: the next tick runs it, once.
	s.db.Model(&store.Group{}).Where("id = ?", g.ID).Update("clean_slot_at", nil)
	s.Tick(ctx)
	s.Tick(ctx)
	s.Wait()
	if n := countRuns(t, s, g.ID, ""); n != 1 {
		t.Fatalf("want exactly one scheduled run, got %d", n)
	}
	var run store.Run
	s.db.Preload("Servers").Where("group_id = ?", g.ID).Take(&run)
	if run.Trigger != string(TriggerSchedule) || run.Kind != string(RunClean) || run.Status == "running" {
		t.Fatalf("run = %+v", run)
	}
	if len(run.Servers) != 1 || run.Servers[0].Status != "failed" {
		t.Fatalf("a server that never enrolled must fail cleanly, got %+v", run.Servers)
	}

	old := time.Now().UTC().Add(-3 * time.Hour).Format("15:04")
	m, err := s.CreateGroup(ctx, a, GroupInput{Name: "missed", Timezone: "UTC",
		Clean:   CleanPolicy{Enabled: true, Days: all, Time: old, BuildCache: true, JournalSize: "200M"},
		Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00"}})
	if err != nil {
		t.Fatal(err)
	}
	addTestServer(t, s, a, &m.ID, "missed-1")
	s.db.Model(&store.Group{}).Where("id = ?", m.ID).Update("clean_slot_at", nil)
	s.Tick(ctx)
	s.Wait()
	if n := countRuns(t, s, m.ID, "missed"); n != 1 {
		t.Fatalf("want one missed run, got %d (all: %d)", n, countRuns(t, s, m.ID, ""))
	}
}

func setSnapshot(t *testing.T, s *Service, srv store.Server, diskUsed uint64) store.ServerState {
	t.Helper()
	now := time.Now()
	snap := proto.Status{
		MemTotal: 100, MemUsed: 10, TimeSynced: true, FailedUnits: []string{},
		Disks: []proto.Disk{{Mount: "/", Total: 100, Used: diskUsed}},
	}
	st := store.ServerState{ServerID: srv.ID, Status: "ok", Snapshot: store.MustJSON(snap), Reasons: store.JSON("[]"), LastSeenAt: &now}
	s.saveState(context.Background(), &st)
	return st
}

// Alerts fire when a condition starts, wait out their duration, and resolve
// when the condition is gone.
func TestAlertsFireAndResolve(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "alerts")
	srv := addTestServer(t, s, a, nil, "disk-1")

	open := func() []AlertView {
		al, err := s.ListAlerts(ctx, a, false, 100)
		if err != nil {
			t.Fatal(err)
		}
		return al
	}

	st := setSnapshot(t, s, srv, 90)
	s.evaluateAlerts(ctx, srv, &st)
	al := open()
	if len(al) != 1 || al[0].Kind != string(AlertDisk) || al[0].FiredAt == nil {
		t.Fatalf("want one firing disk alert, got %+v", al)
	}

	// Same condition again: still one alert, not a second one.
	s.evaluateAlerts(ctx, srv, &st)
	if n := len(open()); n != 1 {
		t.Fatalf("repeated evaluation must not duplicate alerts, got %d", n)
	}

	st = setSnapshot(t, s, srv, 10)
	s.evaluateAlerts(ctx, srv, &st)
	if n := len(open()); n != 0 {
		t.Fatalf("alert must resolve when disk use drops, %d still open", n)
	}

	// A rule with a duration stays pending until the duration passes.
	if _, err := s.UpdateAlertRule(ctx, a, string(AlertDisk), true, 85, 30); err != nil {
		t.Fatal(err)
	}
	st = setSnapshot(t, s, srv, 90)
	s.evaluateAlerts(ctx, srv, &st)
	al = open()
	if len(al) != 1 || al[0].FiredAt != nil {
		t.Fatalf("want one pending alert, got %+v", al)
	}

	// Unreachable servers alert instead of disk.
	st.Status = string(StatusUnreachable)
	st.LastError = "connection refused"
	s.evaluateAlerts(ctx, srv, &st)
	kinds := map[string]bool{}
	for _, x := range open() {
		kinds[x.Kind] = true
	}
	if !kinds[string(AlertUnreachable)] || kinds[string(AlertDisk)] {
		t.Fatalf("want only the unreachable alert, got %v", kinds)
	}
}

func TestMetricsRollup(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "metrics")
	srv := addTestServer(t, s, a, nil, "m-1")

	hour := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	for i, cpu := range []float64{10, 20, 30} {
		smp := store.MetricSample{ServerID: srv.ID, At: hour.Add(time.Duration(i) * time.Minute), CPUPercent: cpu, MemPercent: 50, DiskPercent: float64(40 + i), Load1: 1}
		if err := s.db.Omit("Server").Create(&smp).Error; err != nil {
			t.Fatal(err)
		}
	}
	old := store.MetricSample{ServerID: srv.ID, At: time.Now().Add(-72 * time.Hour), CPUPercent: 1}
	s.db.Omit("Server").Create(&old)

	s.Rollup(ctx)
	var h store.MetricHourly
	if err := s.db.Where("server_id = ? AND hour = ?", srv.ID, hour).Take(&h).Error; err != nil {
		t.Fatalf("no hourly row: %v", err)
	}
	if h.Samples != 3 || h.CPUPercent != 20 || h.DiskPercent != 42 {
		t.Fatalf("hourly = %+v (cpu is the average, disk the maximum)", h)
	}
	var n int64
	s.db.Model(&store.MetricSample{}).Where("server_id = ? AND at < ?", srv.ID, time.Now().Add(-48*time.Hour)).Count(&n)
	if n != 0 {
		t.Fatal("samples older than 48 hours must be deleted")
	}

	pts, err := s.Metrics(ctx, a, srv.ID, 7*24*time.Hour)
	if err != nil || len(pts) != 1 {
		t.Fatalf("7d metrics read hourly rows: %v, %v", pts, err)
	}
}
