package fleet

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// tokenActor is a's account and role, reached through an API token.
func tokenActor(a Actor, scopes ...Scope) Actor {
	a.Source, a.TokenID, a.Scopes = SourceAPI, 1, scopes
	return a
}

func groupWithPlan(t *testing.T, f *roleFixture, checks ...HealthCheck) GroupInput {
	t.Helper()
	in := testGroup("web", "UTC")
	in.Updates.PlanRequired = true
	in.Updates.BatchSize, in.Updates.MaxFailures = 2, 3
	in.Checks = checks
	if _, err := f.s.UpdateGroup(context.Background(), f.owner, f.g.ID, in); err != nil {
		t.Fatal(err)
	}
	return in
}

// A viewer whose team holds a group runs it day to day. Turning off the
// approval it needs is for an organization admin.
func TestTeamViewerCannotTurnOffPlanRequirement(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	in := groupWithPlan(t, f)
	addTeam(t, s, f.org, "web team", []int64{f.vic.UserID}, []int64{f.g.ID})

	weaker := in
	weaker.Updates.PlanRequired = false
	_, err := s.UpdateGroup(ctx, f.vic, f.g.ID, weaker)
	wantErr(t, "team viewer turns off the plan requirement", err, ErrForbidden)
	if _, err := s.StartRun(ctx, f.vic, RunRequest{Kind: RunUpdate, GroupID: &f.g.ID}); err == nil {
		t.Fatal("a team viewer started an update on a group that requires a plan")
	}

	// Other edits by the same viewer still work, and an admin can weaken.
	if _, err := s.UpdateGroup(ctx, f.vic, f.g.ID, in); err != nil {
		t.Errorf("team viewer keeps the requirement and edits: %v", err)
	}
	if _, err := s.UpdateGroup(ctx, f.owner, f.g.ID, weaker); err != nil {
		t.Errorf("owner turns off the requirement: %v", err)
	}
}

// A token cannot loosen the checks and limits an approved plan runs under.
func TestTokenCannotWeakenPlanGroupChecks(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	gate := HealthCheck{Name: "web", Kind: "tcp", Port: 443, Blocking: true}
	in := groupWithPlan(t, f, gate)
	tok := tokenActor(f.owner, ScopeRead, ScopeAdmin)

	noChecks := in
	noChecks.Checks = nil
	_, err := s.UpdateGroup(ctx, tok, f.g.ID, noChecks)
	wantInvalid(t, "token removes a blocking check", err)

	notBlocking := in
	notBlocking.Checks = []HealthCheck{{Name: "web", Kind: "tcp", Port: 443}}
	_, err = s.UpdateGroup(ctx, tok, f.g.ID, notBlocking)
	wantInvalid(t, "token makes a blocking check advisory", err)

	wider := in
	wider.Updates.BatchSize, wider.Updates.MaxFailures = 50, 1000
	_, err = s.UpdateGroup(ctx, tok, f.g.ID, wider)
	wantInvalid(t, "token raises batch size and failure limit", err)

	same := in
	same.Name = "web renamed"
	if _, err := s.UpdateGroup(ctx, tok, f.g.ID, same); err != nil {
		t.Errorf("token makes an unrelated edit: %v", err)
	}
}

// Options on a run may add checks. They cannot drop a blocking one: only a
// person can override a check, with a reason.
func TestRunOptionsKeepBlockingChecks(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	in := testGroup("web", "UTC")
	in.Checks = []HealthCheck{{Name: "web", Kind: "tcp", Port: 443, Blocking: true}}
	if _, err := s.UpdateGroup(ctx, f.owner, f.g.ID, in); err != nil {
		t.Fatal(err)
	}
	for _, who := range []struct {
		name string
		a    Actor
	}{
		{"token", tokenActor(f.owner, ScopeRead, ScopeActions)},
		{"person", f.owner},
	} {
		_, err := s.StartRun(ctx, who.a, RunRequest{Kind: RunUpdate, GroupID: &f.g.ID,
			Options: &RunOptions{Checks: []HealthCheck{}}})
		wantInvalid(t, who.name+" drops every check", err)
		_, err = s.StartRun(ctx, who.a, RunRequest{Kind: RunUpdate, GroupID: &f.g.ID,
			Options: &RunOptions{Checks: []HealthCheck{{Name: "web", Kind: "tcp", Port: 443}}}})
		wantInvalid(t, who.name+" makes the check advisory", err)
	}
	_, err := s.StartRun(ctx, f.owner, RunRequest{Kind: RunUpdate, GroupID: &f.g.ID,
		Options: &RunOptions{Checks: []HealthCheck{
			{Name: "web", Kind: "tcp", Port: 443, Blocking: true},
			{Name: "db", Kind: "tcp", Port: 5432},
		}}})
	if err != nil {
		t.Errorf("adding a check while keeping the blocking one: %v", err)
	}
	s.Wait()
}

// Only a person can change what another team may do with a shared group.
func TestTokenCannotRaiseShareAccess(t *testing.T) {
	f := newShareFixture(t)
	ctx := context.Background()
	sh := f.accept(t, "view")

	tok := tokenActor(f.alice, ScopeRead, ScopeAdmin)
	_, err := f.s.UpdateGroupShare(ctx, tok, sh.ID, "operate")
	wantErr(t, "token raises a share to operate", err, ErrForbidden)
	if _, err := f.s.UpdateGroupShare(ctx, f.alice, sh.ID, "operate"); err != nil {
		t.Errorf("person raises a share: %v", err)
	}
}

// A blocking check is its name and what it checks. Keeping the name while
// pointing the check at something that always passes is a bypass.
func TestBlockingCheckCannotBeRepointed(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	gate := HealthCheck{Name: "web", Kind: "tcp", Port: 443, Blocking: true}
	in := groupWithPlan(t, f, gate)
	tok := tokenActor(f.owner, ScopeRead, ScopeAdmin, ScopeActions)

	repointed := in
	repointed.Checks = []HealthCheck{{Name: "web", Kind: "tcp", Port: 22, Blocking: true}}
	_, err := s.UpdateGroup(ctx, tok, f.g.ID, repointed)
	wantInvalid(t, "token repoints a blocking check", err)

	// The group requires a plan, so use a group that does not for the run.
	free := testGroup("free", "UTC")
	free.Checks = []HealthCheck{gate}
	if _, err := s.UpdateGroup(ctx, f.owner, f.h.ID, free); err != nil {
		t.Fatal(err)
	}
	_, err = s.StartRun(ctx, tok, RunRequest{Kind: RunUpdate, GroupID: &f.h.ID,
		Options: &RunOptions{Checks: []HealthCheck{{Name: "web", Kind: "tcp", Port: 22, Blocking: true}}}})
	wantInvalid(t, "run options repoint a blocking check", err)
}

// An admin who is not an owner cannot send the owner's kept sudo login to a
// server by reinstalling the helper. They enter their own.
func TestReinstallStoredLoginIsOwnerOnly(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	admin := shareMember(t, s, f.org, signIn(t, s, "adam"), RoleAdmin)
	if err := s.db.Model(&f.gSrv).Update("host_key", "ssh-ed25519 AAAA").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.store.InTx(ctx, func(q *store.Queries) error {
		return s.storeLogin(ctx, q, f.gSrv.ID, remote.LoginAuth{User: "root", Password: "kept"})
	}); err != nil {
		t.Fatal(err)
	}
	wantRefusedLogin := func(who string, a Actor) {
		t.Helper()
		_, err := s.ReinstallHelper(ctx, a, f.gSrv.ID, nil)
		var v *ValidationError
		if !errors.As(err, &v) || !strings.Contains(v.Message, "kept for this server") {
			t.Errorf("%s reinstalls with the kept login: %v", who, err)
		}
	}
	wantRefusedLogin("an admin", admin)
	wantRefusedLogin("an owner's token", tokenActor(f.owner, ScopeRead, ScopeAdmin))
}

// A plan that was approved while a rejection was being written stays approved:
// the rejection only changes a plan that is still submitted.
func TestRejectPlanDoesNotOverwriteApproval(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	row := store.FixPlan{AccountID: f.org, Status: "approved", Fingerprint: "fp", CreatedBy: f.owner.UserID, Body: store.JSON("{}"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account").Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	// As if the approval landed after RejectPlan read the plan as submitted.
	ok, err := s.rejectSubmitted(ctx, row.ID, "late")
	if err != nil || ok {
		t.Fatalf("rejecting an approved plan: changed=%v %v", ok, err)
	}
	var got store.FixPlan
	s.db.First(&got, row.ID)
	if got.Status != "approved" {
		t.Fatalf("plan status = %q, want approved", got.Status)
	}
	s.db.Model(&got).Update("status", "submitted")
	if ok, err := s.rejectSubmitted(ctx, row.ID, "no"); err != nil || !ok {
		t.Fatalf("rejecting a submitted plan: changed=%v %v", ok, err)
	}
}

// A run started by server ids keeps the blocking checks of those servers'
// group, the same as a run started for the group.
func TestServerRunKeepsGroupBlockingChecks(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	gate := HealthCheck{Name: "web", Kind: "tcp", Port: 443, Blocking: true}
	in := testGroup("web", "UTC")
	in.Checks = []HealthCheck{gate}
	if _, err := s.UpdateGroup(ctx, f.owner, f.g.ID, in); err != nil {
		t.Fatal(err)
	}
	tok := tokenActor(f.owner, ScopeRead, ScopeActions)
	_, err := s.StartRun(ctx, tok, RunRequest{Kind: RunUpdate, ServerIDs: []int64{f.gSrv.ID}, Options: &RunOptions{Checks: []HealthCheck{}}})
	wantInvalid(t, "server run drops the group's blocking check", err)
	run, err := s.StartRun(ctx, tok, RunRequest{Kind: RunUpdate, ServerIDs: []int64{f.gSrv.ID}})
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	got, err := s.GetRun(ctx, f.owner, run.ID)
	if err != nil || len(got.Options.Checks) != 1 || got.Options.Checks[0].Name != "web" || !got.Options.Checks[0].Blocking {
		t.Fatalf("server run checks = %+v, %v", got.Options.Checks, err)
	}
	// Servers of a group with blocking checks do not mix with others.
	_, err = s.StartRun(ctx, tok, RunRequest{Kind: RunUpdate, ServerIDs: []int64{f.gSrv.ID, f.hSrv.ID}})
	wantInvalid(t, "server run across groups with blocking checks", err)
}

// A token cannot move servers out of a plan group into one with weaker checks
// or limits, even when that group also requires a plan.
func TestTokenCannotMoveServerToWeakerPlanGroup(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	groupWithPlan(t, f, HealthCheck{Name: "web", Kind: "tcp", Port: 443, Blocking: true})
	weak := testGroup("db", "UTC")
	weak.Updates.PlanRequired = true
	weak.Updates.BatchSize, weak.Updates.MaxFailures = 2, 3
	if _, err := s.UpdateGroup(ctx, f.owner, f.h.ID, weak); err != nil {
		t.Fatal(err)
	}
	tok := tokenActor(f.owner, ScopeRead, ScopeAdmin)
	_, err := s.UpdateServer(ctx, tok, f.gSrv.ID, UpdateServerInput{GroupID: &f.h.ID})
	wantInvalid(t, "token moves a server to a plan group without its checks", err)
	if _, err := s.UpdateServer(ctx, f.owner, f.gSrv.ID, UpdateServerInput{GroupID: &f.h.ID}); err != nil {
		t.Errorf("a person moves the server: %v", err)
	}
}

// Clearing a flag that was never set changes nothing, so a token may send it.
func TestTokenMaySendUnsetControlPlaneFlag(t *testing.T) {
	f := newRoleFixture(t)
	off := false
	if _, err := f.s.UpdateServer(context.Background(), tokenActor(f.owner, ScopeRead, ScopeAdmin), f.gSrv.ID, UpdateServerInput{HostsControlPlane: &off}); err != nil {
		t.Fatalf("token sends hostsControlPlane=false on a server without it: %v", err)
	}
}

// Accepting a changed host key is the defence against someone in the middle.
// Only a person can do it.
func TestAcceptHostKeyIsForPeople(t *testing.T) {
	f := newRoleFixture(t)
	_, err := f.s.AcceptHostKey(context.Background(), tokenActor(f.owner, ScopeRead, ScopeAdmin), f.gSrv.ID, "SHA256:x")
	var v *ValidationError
	if !errors.As(err, &v) || !strings.Contains(v.Message, "only a person") {
		t.Fatalf("token accepts a host key: %v", err)
	}
}
