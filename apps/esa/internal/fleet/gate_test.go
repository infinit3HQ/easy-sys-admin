package fleet

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func gateRun(t *testing.T, s *Service, a Actor) (store.Run, RunOptions) {
	t.Helper()
	run, opts := canaryRun(t, s, a, 3)
	opts.WaitAfterCanaries = true
	return run, opts
}

// succeed marks the server done and counts how many started.
func succeed(s *Service, started *int) func(context.Context, *store.RunServer) bool {
	return func(ctx context.Context, rs *store.RunServer) bool {
		*started++
		s.setRunServer(ctx, rs, "succeeded", "", nil, 0)
		return true
	}
}

func waitForGate(t *testing.T, s *Service, runID int64) {
	t.Helper()
	for range 200 {
		var r store.Run
		s.db.Where("id = ?", runID).Take(&r)
		if r.GateState == GateWaiting {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the run never reached its gate")
}

func TestGateContinueRunsTheRest(t *testing.T) {
	gatePollEvery = 20 * time.Millisecond
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "gate-go")
	run, opts := gateRun(t, s, a)
	token := a
	token.Source, token.TokenID, token.Scopes = SourceAPI, 3, []Scope{ScopeActions}
	started := 0
	done := make(chan struct{})
	go func() {
		s.batched(ctx, run.Servers, opts, nil, succeed(s, &started))
		close(done)
	}()
	waitForGate(t, s, run.ID)
	if _, err := s.DecideGate(ctx, token, run.ID, "continue", ""); err == nil {
		t.Fatal("a token let the update spread")
	}
	if _, err := s.DecideGate(ctx, a, run.ID, "continue", "canary looks fine"); err != nil {
		t.Fatal(err)
	}
	<-done
	if started != 3 {
		t.Fatalf("after continue every server runs, %d started", started)
	}
	v, _ := s.GetRun(ctx, a, run.ID)
	if v.Gate == nil || v.Gate.State != GateContinued || v.Gate.Note != "canary looks fine" || v.Gate.By == "" {
		t.Fatalf("gate: %+v", v.Gate)
	}
	// After a restart the decision stands: no second wait.
	again := 0
	s.batched(ctx, run.Servers, opts, nil, succeed(s, &again))
	if again != 0 {
		t.Fatalf("nothing is left to run, %d started", again)
	}
}

func TestGateStopSkipsTheRest(t *testing.T) {
	gatePollEvery = 20 * time.Millisecond
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "gate-stop")
	run, opts := gateRun(t, s, a)
	started := 0
	done := make(chan struct{})
	go func() {
		s.batched(ctx, run.Servers, opts, nil, succeed(s, &started))
		close(done)
	}()
	waitForGate(t, s, run.ID)
	if _, err := s.DecideGate(ctx, a, run.ID, "stop", "error rate went up"); err != nil {
		t.Fatal(err)
	}
	<-done
	s.finishRun(ctx, run.ID)
	got := runStatus(t, s, run.ID)
	if started != 1 || got.Status != "partial" {
		t.Fatalf("started %d, status %s", started, got.Status)
	}
	for _, rs := range got.Servers[1:] {
		if rs.Status != "skipped" || !strings.Contains(rs.Error, "canary gate") {
			t.Fatalf("after stop: %s %q", rs.Status, rs.Error)
		}
	}
	if _, err := s.DecideGate(ctx, a, run.ID, "continue", ""); err == nil {
		t.Fatal("a decided gate was decided again")
	}
}

func TestUnansweredGateStops(t *testing.T) {
	gatePollEvery = 20 * time.Millisecond
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "gate-expire")
	run, opts := gateRun(t, s, a)
	old := s.now().Add(-25 * time.Hour)
	if err := s.db.Model(&store.Run{}).Where("id = ?", run.ID).Updates(map[string]any{"gate_state": GateWaiting, "gate_at": old}).Error; err != nil {
		t.Fatal(err)
	}
	started := 0
	s.batched(ctx, run.Servers, opts, nil, succeed(s, &started))
	got := runStatus(t, s, run.ID)
	if started != 1 || got.GateState != GateExpired || !strings.Contains(got.Servers[2].Error, "nobody answered") {
		t.Fatalf("started %d gate %s err %q", started, got.GateState, got.Servers[2].Error)
	}
}

func TestStoppingProcessLeavesGateRunOpen(t *testing.T) {
	gatePollEvery = 20 * time.Millisecond
	s := newTestService(t)
	a := signIn(t, s, "gate-restart")
	run, opts := gateRun(t, s, a)
	ctx, cancel := context.WithCancel(context.Background())
	started := 0
	done := make(chan struct{})
	go func() {
		s.batched(ctx, run.Servers, opts, nil, succeed(s, &started))
		close(done)
	}()
	waitForGate(t, s, run.ID)
	cancel()
	<-done
	got := runStatus(t, s, run.ID)
	if got.GateState != GateWaiting || got.Servers[1].Status != "queued" {
		t.Fatalf("a stopping process must leave the gate waiting: %s %s", got.GateState, got.Servers[1].Status)
	}
}
