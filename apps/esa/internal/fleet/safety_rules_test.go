package fleet

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// canaryRun stores a running update run on n new servers and returns it
// with its servers loaded, the first server being the canary.
func canaryRun(t *testing.T, s *Service, owner Actor, n int) (store.Run, RunOptions) {
	t.Helper()
	ctx := context.Background()
	var servers []store.Server
	for i := range n {
		srv := store.Server{AccountID: owner.AccountID, Name: fmt.Sprintf("web-%d", i), Host: fmt.Sprintf("10.0.1.%d", i+1), Port: 22,
			Username: "esa", Enrollment: "pending", ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
			t.Fatal(err)
		}
		servers = append(servers, srv)
	}
	opts := RunOptions{BatchSize: 1, MaxFailures: 5, CanaryServerIDs: []int64{servers[0].ID}}
	run, err := s.createRun(ctx, owner, owner.AccountID, RunUpdate, string(TriggerManual), nil, servers, opts, "running")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Preload("Servers", byID).Preload("Servers.Server").Where("id = ?", run.ID).Take(&run).Error; err != nil {
		t.Fatal(err)
	}
	return run, opts
}

func runStatus(t *testing.T, s *Service, id int64) store.Run {
	t.Helper()
	var run store.Run
	if err := s.db.Preload("Servers", byID).Where("id = ?", id).Take(&run).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func TestSkippedCanaryStopsTheRunAndFails(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "canary-skip")
	run, opts := canaryRun(t, s, owner, 3)
	started := 0
	s.batched(ctx, run.Servers, opts, nil, func(ctx context.Context, rs *store.RunServer) bool {
		started++
		// Another run holds the canary: begin records a skip and runUpdate
		// reports it as not failed.
		s.setRunServer(ctx, rs, "skipped", "another run is working on this server", nil, 0)
		return true
	})
	if started != 1 {
		t.Fatalf("only the canary may start, %d servers started", started)
	}
	s.finishRun(ctx, run.ID)
	got := runStatus(t, s, run.ID)
	if got.Status != "failed" {
		t.Fatalf("a run whose canary never ran is %q, want failed", got.Status)
	}
	for _, rs := range got.Servers[1:] {
		if rs.Status != "skipped" || !strings.Contains(rs.Error, "canary did not finish") {
			t.Fatalf("server after the canary: %s %q", rs.Status, rs.Error)
		}
	}
}

func TestFailedCanaryStopsTheRest(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "canary-fail")
	run, opts := canaryRun(t, s, owner, 3)
	s.batched(ctx, run.Servers, opts, nil, func(ctx context.Context, rs *store.RunServer) bool {
		s.setRunServer(ctx, rs, "failed", "check after upgrade: http down", nil, 0)
		return false
	})
	s.finishRun(ctx, run.ID)
	got := runStatus(t, s, run.ID)
	if got.Status != "failed" {
		t.Fatalf("run status %q, want failed", got.Status)
	}
	for _, rs := range got.Servers[1:] {
		if rs.Status != "skipped" || !strings.HasPrefix(rs.Error, "stopped after canary") {
			t.Fatalf("server after the canary: %s %q", rs.Status, rs.Error)
		}
	}
}

// After a restart the canary may already be finished. Its result still
// decides whether the rest start.
func TestResumedRunKeepsCanaryRules(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "canary-resume")
	run, opts := canaryRun(t, s, owner, 3)
	s.setRunServer(ctx, &run.Servers[0], "failed", "upgrade failed", nil, 0)
	started := 0
	s.batched(ctx, run.Servers, opts, []int{0}, func(ctx context.Context, rs *store.RunServer) bool {
		started++
		return true
	})
	if started != 0 {
		t.Fatalf("%d servers started after the canary failed before the restart", started)
	}
}

func TestStoppedRunIsNotSucceeded(t *testing.T) {
	servers := []store.RunServer{
		{Status: "succeeded"},
		{Status: "skipped", Error: "stopped after the watch: web-0: http down"},
	}
	ok, failed, _, _ := runTally(RunUpdate, servers)
	if got := runOutcome(ok, failed, runAborted(servers)); got != "partial" {
		t.Fatalf("a run the watch stopped is %q, want partial", got)
	}
	if got := runOutcome(1, 0, false); got != "succeeded" {
		t.Fatalf("a clean run is %q", got)
	}
}

func TestTokenCannotWeakenPlanRequirement(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "plan-weaken")
	in := GroupInput{Name: "gated", Timezone: "UTC",
		Clean:   CleanPolicy{Days: []string{"sun"}, Time: "03:00", JournalSize: "200M"},
		Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00", PlanRequired: true}}
	g, err := s.CreateGroup(ctx, owner, in)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateGroup(ctx, owner, GroupInput{Name: "open", Timezone: "UTC",
		Clean:   CleanPolicy{Days: []string{"sun"}, Time: "03:00", JournalSize: "200M"},
		Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := store.Server{AccountID: owner.AccountID, GroupID: &g.ID, Name: "db", Host: "10.0.2.1", Port: 22, Username: "esa",
		Enrollment: "pending", ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	token := owner
	token.Source, token.TokenID, token.Scopes = SourceAPI, 7, []Scope{ScopeRead, ScopeActions, ScopeAdmin}

	off := in
	off.Updates.PlanRequired = false
	if _, err := s.UpdateGroup(ctx, token, g.ID, off); err == nil {
		t.Fatal("an admin token turned off the plan requirement")
	}
	scheduled := in
	scheduled.Updates.Enabled = true
	if _, err := s.UpdateGroup(ctx, token, g.ID, scheduled); err == nil {
		t.Fatal("a schedule on a group that requires a plan was accepted")
	}
	if _, err := s.UpdateServer(ctx, token, srv.ID, UpdateServerInput{GroupID: &other.ID}); err == nil {
		t.Fatal("a token moved a server out of a group that requires a plan")
	}
	if _, err := s.UpdateServer(ctx, token, srv.ID, UpdateServerInput{ClearGroup: true}); err == nil {
		t.Fatal("a token took a server out of a group that requires a plan")
	}
	if err := s.DeleteGroup(ctx, token, g.ID); err == nil {
		t.Fatal("a token deleted a group that requires a plan")
	}
	if _, err := s.StartRun(ctx, System(owner.AccountID), RunRequest{Kind: RunUpdate, GroupID: &g.ID}); err == nil {
		t.Fatal("a scheduled run skipped the plan requirement")
	}
	if _, err := s.UpdateGroup(ctx, owner, g.ID, off); err != nil {
		t.Fatalf("a person in the web app could not turn it off: %v", err)
	}
	var entry store.AuditEntry
	if err := s.db.Where("action = ?", "group.update").Order("id desc").Take(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entry.Detail, `"plan_required": false`) && !strings.Contains(entry.Detail, `"plan_required":false`) {
		t.Fatalf("the audit entry does not record the change: %s", entry.Detail)
	}
	s.Wait()
}

func TestGroupPlanPinsItsServers(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "plan-pin")
	g, err := s.CreateGroup(ctx, owner, GroupInput{Name: "pinned", Timezone: "UTC",
		Clean:   CleanPolicy{Days: []string{"sun"}, Time: "03:00", JournalSize: "200M"},
		Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00", PlanRequired: true}})
	if err != nil {
		t.Fatal(err)
	}
	add := func(name string) store.Server {
		srv := store.Server{AccountID: owner.AccountID, GroupID: &g.ID, Name: name, Host: "10.0.3." + name[len(name)-1:], Port: 22, Username: "esa",
			Enrollment: "pending", ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
			t.Fatal(err)
		}
		return srv
	}
	first := add("web-1")
	plan, err := s.ProposePlan(ctx, owner, PlanBody{Kind: RunClean, GroupID: &g.ID, BuildCache: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Body.PinnedServerIDs) != 1 || plan.Body.PinnedServerIDs[0] != first.ID {
		t.Fatalf("pinned: %v", plan.Body.PinnedServerIDs)
	}
	if _, err := s.ApprovePlan(ctx, owner, plan.ID); err != nil {
		t.Fatal(err)
	}
	add("web-2")
	exec := owner
	exec.Source, exec.TokenID, exec.Scopes = SourceAPI, 4, []Scope{ScopeActions}
	if _, err := s.ExecutePlan(ctx, exec, plan.ID); err == nil {
		t.Fatal("a server added after approval was not refused")
	}
	got, err := s.GetPlan(ctx, owner, plan.ID)
	if err != nil || got.Status != "invalidated" {
		t.Fatalf("a refused plan must stay invalidated, got %q (%v)", got.Status, err)
	}
	s.Wait()
}

func TestProposeNeedsTheActionsScope(t *testing.T) {
	s := newTestService(t)
	owner := signIn(t, s, "plan-scope")
	for _, scopes := range [][]Scope{{ScopeRead}, {ScopeRead, ScopeLogs}, {ScopeRead, ScopeActions, ScopeLogs}} {
		token := owner
		token.Source, token.TokenID, token.Scopes = SourceMCP, 5, scopes
		if _, err := s.ProposePlan(context.Background(), token, PlanBody{Kind: RunMeasure, ServerIDs: []int64{1}}); err == nil {
			t.Fatalf("a token with %v proposed a plan", scopes)
		}
	}
}

func TestReviewedNoticeOpensAgainOnANewEvent(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "notice-reopen")
	srv := store.Server{AccountID: owner.AccountID, Name: "web", Host: "10.0.4.1", Port: 22, Username: "esa",
		Enrollment: "pending", ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	if !s.openIncident(ctx, owner.AccountID, srv.ID, "new_key", "SHA256:a on deploy", "key on deploy", false) {
		t.Fatal("first notice not opened")
	}
	list, _ := s.ListIncidents(ctx, owner, false)
	if _, err := s.AcknowledgeIncident(ctx, owner, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if s.openIncident(ctx, owner.AccountID, srv.ID, "new_key", "SHA256:a on deploy", "key on deploy", false) {
		t.Fatal("an unchanged condition opened a reviewed notice")
	}
	if !s.openIncident(ctx, owner.AccountID, srv.ID, "new_key", "SHA256:a on deploy", "key on deploy, now unrestricted", false) {
		t.Fatal("a changed message did not open the reviewed notice")
	}
	list, _ = s.ListIncidents(ctx, owner, false)
	if len(list) != 1 || list[0].State != "open" || !list[0].CanReview {
		t.Fatalf("reopened: %+v", list)
	}
	if _, err := s.AcknowledgeIncident(ctx, owner, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if !s.openIncident(ctx, owner.AccountID, srv.ID, "host_key", "host key", "key changed again", true) {
		t.Fatal("a new host key notice was not opened")
	}
}

func TestSameKeyOnTwoLoginsIsTwoGrants(t *testing.T) {
	access := proto.AccessReport{Users: []proto.LocalUser{
		{Name: "deploy", AuthorizedKeys: []proto.AuthorizedKey{{Fingerprint: "SHA256:a", From: "10.0.0.0/8"}}},
		{Name: "root", AuthorizedKeys: []proto.AuthorizedKey{{Fingerprint: "SHA256:a"}}},
	}}
	keys := keyUses(access)
	if len(keys) != 2 {
		t.Fatalf("keys: %+v", keys)
	}
	fresh := freshKeys([]string{"SHA256:a deploy"}, keys)
	if len(fresh) != 1 || fresh[0] != "SHA256:a on root" {
		t.Fatalf("the key added to root must be new: %v", fresh)
	}
	if got := freshKeys([]string{"SHA256:a"}, keys); len(got) != 0 {
		t.Fatalf("an old fingerprint-only baseline must not alert on upgrade: %v", got)
	}
}
