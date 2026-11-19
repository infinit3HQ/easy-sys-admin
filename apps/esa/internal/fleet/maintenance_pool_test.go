package fleet

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"
)

// GitHub's small runners expose the pgx default of four connections. A run
// lock plus cleanup workers used to consume every session, then wait forever
// for an ordinary query before any worker could release its session.
func TestMaintenanceProgressWithSmallDatabasePools(t *testing.T) {
	raw := os.Getenv("ESA_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set ESA_TEST_DATABASE_URL to a disposable PostgreSQL database")
	}
	dsn, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	query := dsn.Query()
	query.Set("pool_max_conns", "2")
	dsn.RawQuery = query.Encode()
	t.Setenv("ESA_TEST_DATABASE_URL", dsn.String())
	s := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s.baseCtx = ctx
	owner := signIn(t, s, "small-maintenance-pool")
	var ids []int64
	for _, name := range []string{"pool-a", "pool-b", "pool-c", "pool-d", "pool-e"} {
		srv := addTestServer(t, s, owner, nil, name)
		ids = append(ids, srv.ID)
	}
	// More simultaneous runs than run-lock slots, and more servers in each
	// run than server-lock slots. All must finish without starving queries.
	var runs []int64
	for i := 0; i < 3; i++ {
		run, err := s.StartRun(ctx, owner, RunRequest{Kind: RunMeasure, ServerIDs: ids})
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, run.ID)
	}
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("maintenance exhausted the database connection pools")
	}
	for _, id := range runs {
		run, err := s.GetRun(ctx, owner, id)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == "running" || run.Status == "queued" {
			t.Fatalf("run %d did not finish: %s", id, run.Status)
		}
	}
}
