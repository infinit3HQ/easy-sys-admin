package fleet

import (
	"context"
	"testing"
	"time"

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

	_, err = s.StartRun(ctx, tok, RunRequest{Kind: RunMeasure, GroupID: &f.g.ID,
		Options: &RunOptions{Checks: []HealthCheck{{Name: "web", Kind: "tcp", Port: 22, Blocking: true}}}})
	wantInvalid(t, "run options repoint a blocking check", err)
}

// A plan is approved to run with the group's reboot and hold settings.
func TestTokenCannotWidenPlanGroupReboot(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	in := groupWithPlan(t, f)
	in.Updates.Hold = []string{"linux-image-generic"}
	if _, err := s.UpdateGroup(ctx, f.owner, f.g.ID, in); err != nil {
		t.Fatal(err)
	}
	tok := tokenActor(f.owner, ScopeRead, ScopeAdmin)

	reboot := in
	reboot.Updates.RebootIfRequired = true
	_, err := s.UpdateGroup(ctx, tok, f.g.ID, reboot)
	wantInvalid(t, "token turns on reboot", err)

	unheld := in
	unheld.Updates.Hold = nil
	_, err = s.UpdateGroup(ctx, tok, f.g.ID, unheld)
	wantInvalid(t, "token drops a held package", err)
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
	_, err := s.ReinstallHelper(ctx, admin, f.gSrv.ID, nil)
	wantInvalid(t, "admin reinstalls with the owner's kept login", err)
}

// A plan that was approved while a rejection was being written stays approved.
func TestRejectPlanDoesNotOverwriteApproval(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	row := store.FixPlan{AccountID: f.org, Status: "approved", Fingerprint: "fp", CreatedBy: f.owner.UserID, Body: store.JSON("{}"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account").Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.RejectPlan(ctx, f.owner, row.ID, "late"); err == nil {
		t.Fatal("an approved plan was rejected")
	}
	var got store.FixPlan
	s.db.First(&got, row.ID)
	if got.Status != "approved" {
		t.Fatalf("plan status = %q, want approved", got.Status)
	}
}
