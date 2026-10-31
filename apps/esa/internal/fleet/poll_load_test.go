//go:build unix

package fleet

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestPollLoad measures how many servers one poll cycle reaches. It starts
// in-process SSH servers that answer `esa-helper status` after 1.2 seconds,
// like a real server's CPU sample, plus dead ones: listeners that accept TCP
// and never speak (a hung sshd, held for the 15 second handshake timeout)
// and closed ports (refused at once). Each cycle runs with the 55 second
// deadline the poll job uses.
//
// It is slow (about 10 minutes) and needs ESA_TEST_DATABASE_URL and
// ESA_LOAD_TEST=1. It never runs with -short.
//
//	ESA_LOAD_TEST=1 go test -run Load -v -timeout 30m ./internal/fleet/
func TestPollLoad(t *testing.T) {
	if testing.Short() || os.Getenv("ESA_LOAD_TEST") != "1" {
		t.Skip("set ESA_LOAD_TEST=1 (and ESA_TEST_DATABASE_URL) to run the poll load test")
	}
	if os.Getenv("ESA_TEST_DATABASE_URL") == "" {
		t.Skip("set ESA_TEST_DATABASE_URL to a disposable PostgreSQL database to run this test")
	}
	const (
		statusDelay = 1200 * time.Millisecond
		cycle       = 55 * time.Second // jobs.go: one minute minus a twelfth
		maxN        = 1000
		dead        = 50
	)
	// Each healthy server needs a listener plus both ends of one connection.
	ensureFiles(t, 4*(maxN+dead)+512)

	f := newFakeFleet(t, statusDelay)
	healthy := make([]fakeHost, maxN)
	for i := range healthy {
		healthy[i] = f.healthy()
	}
	var deadHosts []fakeHost
	for i := range dead {
		if i%2 == 0 {
			deadHosts = append(deadHosts, f.silent())
		} else {
			deadHosts = append(deadHosts, f.closedPort())
		}
	}

	type result struct {
		n, conc, dead int
		phase         string
		ok, failed    uint64
		deferred      uint64
		skipped       int
		seconds       float64
	}
	var results []result

	for _, n := range []int{300, 1000} {
		for _, conc := range []int{8, 64} {
			for _, withDead := range []bool{false, true} {
				hosts := append([]fakeHost(nil), healthy[:n]...)
				if withDead {
					hosts = append(hosts, deadHosts...)
				}
				s := newTestService(t)
				s.allowPrivate = true
				s.pollConcurrency = conc
				a := signIn(t, s, fmt.Sprintf("load-%d-%d-%v", n, conc, withDead))
				acceptAccountKey(t, s, f, a.AccountID)
				// Dead servers start with 2 missed polls, so the first cycle
				// is the third miss and the next one runs with the back-off.
				rng := rand.New(rand.NewPCG(uint64(n), uint64(conc)))
				base := time.Now().Add(-2 * time.Minute)
				ids := insertFakeServers(t, s, a.AccountID, hosts,
					func(i int) int {
						if i >= n {
							return 2
						}
						return 0
					},
					func(int) time.Time { return base.Add(time.Duration(rng.IntN(60_000)) * time.Millisecond) })

				phases := []string{"no dead"}
				if withDead {
					phases = []string{"50 dead, before back-off", "50 dead, after back-off"}
				}
				for _, phase := range phases {
					if phase == "50 dead, after back-off" {
						// A short cycle does not reach every dead server, and
						// one it never reached is tried first next time. After
						// a few cycles every dead server is backed off; start
						// from there to measure the steady state.
						s.startCycle()
						for _, id := range ids[n:] {
							s.recordUnreachable(id)
						}
					}
					before := s.rt().stats
					ctx, cancel := context.WithTimeout(context.Background(), cycle)
					s.PollAll(ctx)
					cancel()
					st := s.rt().stats
					r := result{n: n, conc: conc, dead: len(hosts) - n, phase: phase,
						ok: st.OK - before.OK, failed: st.Failed - before.Failed, deferred: st.Deferred - before.Deferred,
						skipped: st.LastCycleSkipped, seconds: st.LastCycleSeconds}
					results = append(results, r)
					t.Logf("N=%d concurrency=%d %s: %d polled ok, %d failed, %d deferred, %d not started, cycle %.1fs",
						r.n, r.conc, r.phase, r.ok, r.failed, r.deferred, r.skipped, r.seconds)
					if n == maxN && conc == 64 && !withDead && (r.ok != maxN || r.skipped != 0) {
						t.Errorf("concurrency 64 must poll all %d healthy servers in one cycle, got %d ok, %d not started", maxN, r.ok, r.skipped)
					}
				}
				s.pool.Close()
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n| Healthy | Concurrency | Case | Polled ok | Failed | Deferred | Not started | Cycle |\n")
	fmt.Fprintf(&b, "| ---: | ---: | --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, r := range results {
		fmt.Fprintf(&b, "| %d | %d | %s | %d | %d | %d | %d | %.1f s |\n", r.n, r.conc, r.phase, r.ok, r.failed, r.deferred, r.skipped, r.seconds)
	}
	t.Log(b.String())
}

// ensureFiles raises the soft open-file limit to at least n, or skips.
func ensureFiles(t *testing.T, n uint64) {
	var lim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &lim); err != nil {
		t.Fatal(err)
	}
	if lim.Cur >= n {
		t.Logf("open file limit %d, need about %d", lim.Cur, n)
		return
	}
	want := lim
	want.Cur = min(n, lim.Max)
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &want); err != nil || want.Cur < n {
		t.Skipf("open file limit is %d and cannot be raised to %d (max %d); run ulimit -n %d first", lim.Cur, n, lim.Max, n)
	}
	t.Logf("raised the open file limit from %d to %d", lim.Cur, want.Cur)
}
