package fleet

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func testRunServers(names ...string) []store.RunServer {
	out := make([]store.RunServer, len(names))
	for i, n := range names {
		out[i] = store.RunServer{Status: "queued", Server: store.Server{Name: n, HostsControlPlane: n == "cp"}}
	}
	return out
}

func allIndexes(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// Batches keep the group's order, hold at most the batch size, and put the
// server that hosts the control plane in a batch of its own.
func TestPlanBatches(t *testing.T) {
	cases := []struct {
		name  string
		items []store.RunServer
		idx   []int
		size  int
		want  [][]int
	}{
		{"one at a time", testRunServers("a", "b", "c"), nil, 1, [][]int{{0}, {1}, {2}}},
		{"zero means one", testRunServers("a", "b"), nil, 0, [][]int{{0}, {1}}},
		{"even split", testRunServers("a", "b", "c", "d"), nil, 2, [][]int{{0, 1}, {2, 3}}},
		{"short last batch", testRunServers("a", "b", "c", "d", "e"), nil, 2, [][]int{{0, 1}, {2, 3}, {4}}},
		{"bigger than the group", testRunServers("a", "b", "c"), nil, 50, [][]int{{0, 1, 2}}},
		{"control plane alone", testRunServers("a", "b", "cp", "c", "d", "e"), nil, 3, [][]int{{0, 1}, {2}, {3, 4, 5}}},
		{"control plane first", testRunServers("cp", "a", "b"), nil, 5, [][]int{{0}, {1, 2}}},
		{"control plane last", testRunServers("a", "b", "cp"), nil, 2, [][]int{{0, 1}, {2}}},
		{"subset only", testRunServers("a", "b", "c", "d", "e"), []int{1, 3, 4}, 2, [][]int{{1, 3}, {4}}},
		{"nothing to do", testRunServers("a"), []int{}, 2, nil},
	}
	for _, c := range cases {
		idx := c.idx
		if idx == nil {
			idx = allIndexes(len(c.items))
		}
		if got := planBatches(c.items, idx, c.size); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// A batch runs its servers at the same time, the next batch waits for the
// whole batch, and no batch starts once the failures reach the limit.
func TestRunBatches(t *testing.T) {
	ctx := context.Background()

	t.Run("parallel inside a batch, never across batches", func(t *testing.T) {
		var mu sync.Mutex
		active, peak := 0, 0
		var order []int
		batches := [][]int{{0, 1, 2}, {3, 4}, {5}}
		batchOf := map[int]int{0: 0, 1: 0, 2: 0, 3: 1, 4: 1, 5: 2}
		var started []int
		release := make(chan struct{})
		go func() {
			// Let the first batch reach its peak before any server finishes.
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
				mu.Lock()
				n := active
				mu.Unlock()
				if n == 3 {
					close(release)
					return
				}
				time.Sleep(time.Millisecond)
			}
		}()
		failed, n := runBatches(ctx, batches, 1, nil, func(_ context.Context, i int) bool {
			mu.Lock()
			for _, j := range started {
				if batchOf[j] < batchOf[i] && !contains(order, j) {
					t.Errorf("server %d started before server %d of an earlier batch finished", i, j)
				}
			}
			started = append(started, i)
			active++
			peak = max(peak, active)
			mu.Unlock()
			if i < 3 {
				select {
				case <-release:
				case <-time.After(5 * time.Second):
					t.Error("the first batch never ran its 3 servers at once")
				}
			}
			mu.Lock()
			active--
			order = append(order, i)
			mu.Unlock()
			return true
		})
		if len(failed) != 0 || n != 3 {
			t.Fatalf("want no failures and 3 batches, got %v and %d", failed, n)
		}
		if peak != 3 {
			t.Errorf("want 3 servers at once in the first batch, got %d", peak)
		}
		if len(order) != 6 {
			t.Errorf("want 6 servers done, got %v", order)
		}
	})

	t.Run("stops after the batch that reaches the limit", func(t *testing.T) {
		fail := map[int]bool{1: true, 2: true, 4: true}
		var ran []int
		var mu sync.Mutex
		work := func(_ context.Context, i int) bool {
			mu.Lock()
			ran = append(ran, i)
			mu.Unlock()
			return !fail[i]
		}
		failed, n := runBatches(ctx, [][]int{{0, 1}, {2, 3}, {4, 5}, {6}}, 2, nil, work)
		if !reflect.DeepEqual(failed, []int{1, 2}) || n != 2 {
			t.Fatalf("want failures [1 2] after 2 batches, got %v after %d", failed, n)
		}
		if len(ran) != 4 {
			t.Errorf("the running batch must finish and no later one start, ran %v", ran)
		}
	})

	t.Run("limit 1 stops at the first failure", func(t *testing.T) {
		failed, n := runBatches(ctx, [][]int{{0}, {1}, {2}}, 1, nil, func(_ context.Context, i int) bool { return i != 1 })
		if !reflect.DeepEqual(failed, []int{1}) || n != 2 {
			t.Fatalf("got %v after %d batches", failed, n)
		}
	})

	t.Run("a high limit runs everything", func(t *testing.T) {
		failed, n := runBatches(ctx, [][]int{{0, 1}, {2}}, 10, nil, func(context.Context, int) bool { return false })
		if len(failed) != 3 || n != 2 {
			t.Fatalf("got %v after %d batches", failed, n)
		}
	})

	t.Run("earlier failures count", func(t *testing.T) {
		called := false
		failed, n := runBatches(ctx, [][]int{{2}, {3}}, 2, []int{0, 1}, func(context.Context, int) bool { called = true; return true })
		if called || n != 0 || len(failed) != 2 {
			t.Fatalf("a run already at its limit must start nothing: called=%v n=%d failed=%v", called, n, failed)
		}
	})

	t.Run("a cancelled run starts nothing", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, n := runBatches(cctx, [][]int{{0}}, 1, nil, func(context.Context, int) bool { t.Error("ran"); return true })
		if n != 0 {
			t.Fatalf("got %d batches", n)
		}
	})
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestStopMessage(t *testing.T) {
	items := testRunServers("a", "b", "c")
	if got := stopMessage(items, []int{1}); got != "stopped after b failed" {
		t.Errorf("one failure: %q", got)
	}
	if got := stopMessage(items, []int{0, 2}); got != "stopped after 2 failed servers" {
		t.Errorf("two failures: %q", got)
	}
}

// After a restart, every server still running is followed, and earlier
// failures count toward the limit.
func TestResumeState(t *testing.T) {
	items := testRunServers("a", "b", "c", "d", "e", "f")
	for i, st := range []string{"succeeded", "failed", "running", "running", "queued", "skipped"} {
		items[i].Status = st
	}
	failed, running := resumeState(items)
	if !reflect.DeepEqual(failed, []int{1}) || !reflect.DeepEqual(running, []int{2, 3}) {
		t.Fatalf("got failed %v running %v", failed, running)
	}
}

func TestRunOptionsBatching(t *testing.T) {
	if size, maxF := (RunOptions{}).batching(); size != 1 || maxF != 1 {
		t.Errorf("old runs must default to 1 and 1, got %d and %d", size, maxF)
	}
	for _, c := range []struct {
		size, maxF int
		field      string
	}{{0, 0, ""}, {1, 1, ""}, {50, 1000, ""}, {51, 1, "batchSize"}, {-1, 1, "batchSize"}, {1, 1001, "maxFailures"}, {1, -1, "maxFailures"}} {
		err := validBatching(c.size, c.maxF)
		var verr *ValidationError
		switch {
		case c.field == "" && err != nil:
			t.Errorf("%d/%d: unexpected %v", c.size, c.maxF, err)
		case c.field != "" && (!errors.As(err, &verr) || verr.Field != c.field):
			t.Errorf("%d/%d: want a %s error, got %v", c.size, c.maxF, c.field, err)
		}
	}
}

// Groups store the rolling update settings, and runs copy them unless the
// request overrides them.
func TestGroupBatchSettings(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "batch")
	base := GroupInput{Name: "roll", Timezone: "UTC", Clean: CleanPolicy{Days: []string{"sun"}, Time: "03:30"},
		Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00"}}

	g, err := s.CreateGroup(ctx, a, base)
	if err != nil {
		t.Fatal(err)
	}
	if g.Updates.BatchSize != 1 || g.Updates.MaxFailures != 1 {
		t.Fatalf("defaults: got %d and %d", g.Updates.BatchSize, g.Updates.MaxFailures)
	}

	bad := base
	bad.Updates.BatchSize = 51
	var verr *ValidationError
	if _, err := s.UpdateGroup(ctx, a, g.ID, bad); !errors.As(err, &verr) || verr.Field != "batchSize" {
		t.Fatalf("want a batchSize error, got %v", err)
	}
	bad = base
	bad.Updates.MaxFailures = 1001
	if _, err := s.UpdateGroup(ctx, a, g.ID, bad); !errors.As(err, &verr) || verr.Field != "maxFailures" {
		t.Fatalf("want a maxFailures error, got %v", err)
	}

	in := base
	in.Updates.BatchSize, in.Updates.MaxFailures = 2, 3
	if g, err = s.UpdateGroup(ctx, a, g.ID, in); err != nil {
		t.Fatal(err)
	}
	if g.Updates.BatchSize != 2 || g.Updates.MaxFailures != 3 {
		t.Fatalf("saved: got %d and %d", g.Updates.BatchSize, g.Updates.MaxFailures)
	}

	// The servers have not finished enrollment, so each one fails at once.
	for _, n := range []string{"r-1", "r-2", "r-3", "r-4", "r-5"} {
		addTestServer(t, s, a, &g.ID, n)
	}
	run, err := s.StartRun(ctx, a, RunRequest{Kind: RunUpdate, GroupID: &g.ID})
	if err != nil {
		t.Fatal(err)
	}
	if run.Options.BatchSize != 2 || run.Options.MaxFailures != 3 {
		t.Fatalf("run options: got %d and %d", run.Options.BatchSize, run.Options.MaxFailures)
	}
	s.Wait()
	run, err = s.GetRun(ctx, a, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Two batches of two fail (4 >= 3), so the fifth server is skipped.
	want := []string{"failed", "failed", "failed", "failed", "skipped"}
	for i, rs := range run.Servers {
		if rs.Status != want[i] {
			t.Errorf("%s: got %s, want %s", rs.ServerName, rs.Status, want[i])
		}
	}
	if last := run.Servers[4]; last.Error != "stopped after 4 failed servers" {
		t.Errorf("skip reason: %q", last.Error)
	}

	// An override keeps the group's failure limit when it leaves it out.
	run, err = s.StartRun(ctx, a, RunRequest{Kind: RunReboot, GroupID: &g.ID, Options: &RunOptions{BatchSize: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if run.Options.BatchSize != 1 || run.Options.MaxFailures != 3 {
		t.Fatalf("override: got %d and %d", run.Options.BatchSize, run.Options.MaxFailures)
	}
	s.Wait()
	run, _ = s.GetRun(ctx, a, run.ID)
	want = []string{"failed", "failed", "failed", "skipped", "skipped"}
	for i, rs := range run.Servers {
		if rs.Status != want[i] {
			t.Errorf("reboot %s: got %s, want %s", rs.ServerName, rs.Status, want[i])
		}
	}

	if _, err := s.StartRun(ctx, a, RunRequest{Kind: RunUpdate, GroupID: &g.ID, Options: &RunOptions{BatchSize: 51}}); !errors.As(err, &verr) || verr.Field != "batchSize" {
		t.Fatalf("want a batchSize error for the override, got %v", err)
	}

	// Cleanups do not batch, so their options leave the settings out.
	run, err = s.StartRun(ctx, a, RunRequest{Kind: RunMeasure, GroupID: &g.ID})
	if err != nil {
		t.Fatal(err)
	}
	if run.Options.BatchSize != 0 || run.Options.MaxFailures != 0 {
		t.Fatalf("measure options: got %d and %d", run.Options.BatchSize, run.Options.MaxFailures)
	}
	s.Wait()
}
