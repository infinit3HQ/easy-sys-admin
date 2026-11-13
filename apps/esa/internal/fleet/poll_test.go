package fleet

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// An unreachable server is polled once every 5 cycles; the auth-failure
// back-off still applies and the longer of the two wins.
func TestPollBackoffCombines(t *testing.T) {
	s := &Service{}
	start := s.startCycle()

	s.recordUnreachable(1)
	if !s.pollDeferred(1, start) || !s.pollDeferred(1, start.Add(4*time.Minute)) {
		t.Fatal("an unreachable server must be skipped for the next 4 cycles")
	}
	if s.pollDeferred(1, start.Add(5*time.Minute)) {
		t.Fatal("an unreachable server must be polled on the 5th cycle")
	}
	s.clearBackoff(1)
	if s.pollDeferred(1, start) {
		t.Fatal("clearBackoff must end the back-off")
	}

	// Six auth failures in a row back off for 16 minutes.
	for range 6 {
		s.recordAuthFailure(2)
	}
	s.recordUnreachable(2)
	if !s.pollDeferred(2, start.Add(10*time.Minute)) {
		t.Fatal("the unreachable back-off must not shorten a longer auth back-off")
	}

	// A server that answers again is not deferred.
	if s.pollDeferred(3, start) {
		t.Fatal("a server with no failures is deferred")
	}
}

func TestPollBackoffSurvivesRestart(t *testing.T) {
	s := newTestService(t)
	a := signIn(t, s, "backoff-restart")
	srv := addTestServer(t, s, a, nil, "b1")
	setSnapshot(t, s, srv, 10)
	for range 3 {
		s.recordAuthFailure(srv.ID)
	}
	s.rt().backoffMu.Lock()
	s.rt().backoff = map[int64]*pollBackoff{}
	s.rt().backoffMu.Unlock()
	if !s.pollDeferred(srv.ID, time.Now()) {
		t.Fatal("a restart would try a login that just failed")
	}
	s.clearBackoff(srv.ID)
	s.rt().backoffMu.Lock()
	s.rt().backoff = map[int64]*pollBackoff{}
	s.rt().backoffMu.Unlock()
	if s.pollDeferred(srv.ID, time.Now()) {
		t.Fatal("clearing the back-off did not stick")
	}
}

func TestUpdatesAreStale(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	if !UpdatesAreStale(nil, now) {
		t.Fatal("a server that has never been checked is current")
	}
	recent := now.Add(-6 * time.Hour)
	if UpdatesAreStale(&recent, now) {
		t.Fatal("one 6 hour interval is stale")
	}
	old := now.Add(-13 * time.Hour)
	if !UpdatesAreStale(&old, now) {
		t.Fatal("two missed checks are still current")
	}
}

func TestOnlyOneProcessPolls(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	if !s.claimJob(ctx, "poll", time.Minute) {
		t.Fatal("the first process did not take the poll")
	}
	other := &Service{db: s.db, log: s.log, now: s.now}
	if other.claimJob(ctx, "poll", time.Minute) {
		t.Fatal("a second process polled while the first still holds it")
	}
	if err := s.db.Model(&store.JobLease{}).Where("name = ?", "poll").Update("until", s.now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if !other.claimJob(ctx, "poll", time.Minute) {
		t.Fatal("an expired poll lease was not taken over")
	}
	if !other.claimJob(ctx, "poll", time.Minute) {
		t.Fatal("the holder could not renew")
	}
}

func TestSecondProcessResumesADeadRun(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "resume-dead")
	srv := addTestServer(t, s, a, nil, "r1")
	run := store.Run{AccountID: a.AccountID, Kind: "clean", Trigger: "manual", Status: "running", Options: store.JSON("{}"), StartedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	rs := store.RunServer{RunID: run.ID, ServerID: srv.ID, Status: "running", Detail: store.JSON("{}")}
	if err := s.db.Omit("Run", "Server").Create(&rs).Error; err != nil {
		t.Fatal(err)
	}
	unlock, ok := s.tryRunLock(ctx, run.ID)
	if !ok {
		t.Fatal("lock")
	}
	other := &Service{db: s.db, log: s.log, now: s.now, publicURL: s.publicURL}
	other.ResumeRuns(ctx)
	var still store.RunServer
	if err := s.db.First(&still, rs.ID).Error; err != nil {
		t.Fatal(err)
	}
	if still.Status != "running" {
		t.Fatalf("a live holder was interrupted: %s %s", still.Status, still.Error)
	}
	unlock()
	other.ResumeRuns(ctx)
	if err := s.db.First(&still, rs.ID).Error; err != nil {
		t.Fatal(err)
	}
	if still.Status != "failed" || !strings.Contains(still.Error, "interrupted") {
		t.Fatalf("after the holder died: %s %s", still.Status, still.Error)
	}
}

// A dead server is marked unreachable after 3 missed polls, then deferred.
// Its alert still fires on the next poll after the rule's 3 minutes, and
// resolves on the first poll that succeeds, which also ends the back-off.
func TestUnreachableBackoffAndAlert(t *testing.T) {
	s := newTestService(t)
	s.allowPrivate = true
	ctx := context.Background()
	a := signIn(t, s, "backoff")
	f := newFakeFleet(t, 10*time.Millisecond)
	acceptAccountKey(t, s, f, a.AccountID)
	t.Cleanup(s.pool.Close)

	live, dead := f.healthy(), f.closedPort()
	ids := insertFakeServers(t, s, a.AccountID, []fakeHost{live, dead},
		func(int) int { return 0 }, func(i int) time.Time { return time.Now().Add(-time.Duration(i) * time.Second) })
	liveID, deadID := ids[0], ids[1]

	state := func(id int64) store.ServerState {
		var st store.ServerState
		if err := s.db.Where("server_id = ?", id).Take(&st).Error; err != nil {
			t.Fatal(err)
		}
		return st
	}
	alert := func() *store.Alert {
		var al store.Alert
		if err := s.db.Where("server_id = ? AND kind = ?", deadID, string(AlertUnreachable)).Order("id DESC").Take(&al).Error; err != nil {
			return nil
		}
		return &al
	}

	for cycle := 1; cycle <= 3; cycle++ {
		s.PollAll(ctx)
		if st := state(deadID); st.MissedPolls != cycle {
			t.Fatalf("cycle %d: missed polls %d", cycle, st.MissedPolls)
		}
	}
	st := state(deadID)
	if st.Status != string(StatusUnreachable) {
		t.Fatalf("after 3 misses: status %q", st.Status)
	}
	if s.rt().cycleStart.IsZero() || !s.pollDeferred(deadID, s.rt().cycleStart) {
		t.Fatal("after 3 misses the server must be deferred")
	}
	al := alert()
	if al == nil || al.FiredAt != nil || al.ResolvedAt != nil {
		t.Fatalf("after 3 misses the alert must be pending: %+v", al)
	}

	// The next cycle skips the dead server and still polls the live one.
	before := s.rt().stats
	s.PollAll(ctx)
	after := s.rt().stats
	if after.Deferred != before.Deferred+1 || after.OK != before.OK+1 {
		t.Fatalf("deferred %d->%d, ok %d->%d", before.Deferred, after.Deferred, before.OK, after.OK)
	}
	if got := state(deadID).MissedPolls; got != 3 {
		t.Fatalf("a deferred server must not be contacted: missed polls %d", got)
	}
	if state(liveID).Status != string(StatusOK) {
		t.Fatal("the live server must stay ok")
	}

	// Five minutes later: the alert has been pending longer than its 3
	// minutes, so the next poll fires it.
	s.db.Model(&store.Alert{}).Where("id = ?", al.ID).Update("pending_since", time.Now().Add(-5*time.Minute))
	s.PollServer(ctx, deadID)
	if al = alert(); al == nil || al.FiredAt == nil || al.ResolvedAt != nil {
		t.Fatalf("the unreachable alert must fire on the first poll after 3 minutes: %+v", al)
	}

	stats, err := s.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.ServersByStatus[StatusUnreachable] != 1 || stats.ServersByStatus[StatusOK] != 1 || stats.Poll.Deferred == 0 {
		t.Fatalf("stats: %+v", stats)
	}
	dump := fmt.Sprintf("%+v", stats)
	for _, secret := range []string{"healthy-0", "closed-1", live.Host, "backoff@example.com"} {
		if strings.Contains(dump, secret) {
			t.Fatalf("stats contain %q: %s", secret, dump)
		}
	}

	// The server comes back on the live address: one poll clears everything.
	s.db.Model(&store.Server{}).Where("id = ?", deadID).Updates(map[string]any{
		"host": live.Host, "port": live.Port, "host_key": remote.MarshalKey(live.HostKey), "host_key_fingerprint": remote.Fingerprint(live.HostKey),
	})
	s.PollServer(ctx, deadID)
	if st := state(deadID); st.Status != string(StatusOK) || st.MissedPolls != 0 {
		t.Fatalf("after an answer: status %q, missed %d", st.Status, st.MissedPolls)
	}
	if s.pollDeferred(deadID, time.Now()) {
		t.Fatal("a successful poll must end the back-off")
	}
	if al = alert(); al == nil || al.ResolvedAt == nil {
		t.Fatalf("the alert must resolve: %+v", al)
	}
}
