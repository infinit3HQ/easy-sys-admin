package fleet

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// addTeam creates a team in accountID with the given members and groups.
func addTeam(t *testing.T, s *Service, accountID int64, name string, users []int64, groups []int64) store.Team {
	t.Helper()
	team := store.Team{AccountID: accountID, Name: name, CreatedAt: time.Now()}
	if err := s.db.Omit(clause.Associations).Create(&team).Error; err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if err := s.db.Omit(clause.Associations).Create(&store.TeamMember{TeamID: team.ID, UserID: u, CreatedAt: time.Now()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range groups {
		if err := s.db.Omit(clause.Associations).Create(&store.TeamGroup{TeamID: team.ID, GroupID: g}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return team
}

// testGroup is a valid group input.
func testGroup(name, tz string) GroupInput {
	return GroupInput{Name: name, Timezone: tz,
		Clean: CleanPolicy{Days: []string{"sun"}, Time: "03:00", JournalSize: "200M"}, Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00"}}
}

// wantAllowed checks the permission check passed. Test servers never
// finished enrollment, so an allowed host call stops there.
func wantAllowed(t *testing.T, what string, err error) {
	t.Helper()
	var u *UnavailableError
	if err != nil && !errors.As(err, &u) {
		t.Errorf("%s: want allowed, got %v", what, err)
	}
}

type roleFixture struct {
	s          *Service
	org        int64
	owner      Actor // owner of org
	vic        Actor // viewer of org
	g, h       GroupView
	gSrv, hSrv store.Server
}

func newRoleFixture(t *testing.T) *roleFixture {
	t.Helper()
	s := newTestService(t)
	ctx := context.Background()
	f := &roleFixture{s: s}
	f.org = shareTeam(t, s, "acme")
	f.owner = shareMember(t, s, f.org, signIn(t, s, "olga"), RoleOwner)
	f.vic = shareMember(t, s, f.org, signIn(t, s, "vic"), RoleViewer)
	var err error
	if f.g, err = s.CreateGroup(ctx, f.owner, testGroup("web", "UTC")); err != nil {
		t.Fatal(err)
	}
	if f.h, err = s.CreateGroup(ctx, f.owner, testGroup("db", "UTC")); err != nil {
		t.Fatal(err)
	}
	f.gSrv = addTestServer(t, s, f.owner, &f.g.ID, "web-1")
	f.hSrv = addTestServer(t, s, f.owner, &f.h.ID, "db-1")
	return f
}

// A viewer without a team stays read only.
func TestViewerWithoutTeamCannotAct(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	_, err := s.StartRun(ctx, f.vic, RunRequest{Kind: RunMeasure, GroupID: &f.g.ID})
	wantErr(t, "StartRun on a group", err, ErrForbidden)
	_, err = s.StartRun(ctx, f.vic, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.gSrv.ID}})
	wantErr(t, "StartRun on a server", err, ErrForbidden)
	wantErr(t, "ServiceAction", s.ServiceAction(ctx, f.vic, f.gSrv.ID, "nginx.service", "restart"), ErrForbidden)
	_, err = s.Logs(ctx, f.vic, f.gSrv.ID, LogQuery{Unit: "ssh.service"})
	wantErr(t, "Logs", err, ErrForbidden)
	_, err = s.UpdateGroup(ctx, f.vic, f.g.ID, testGroup("web", "UTC"))
	wantErr(t, "UpdateGroup", err, ErrForbidden)

	v, err := s.GetServer(ctx, f.vic, f.gSrv.ID)
	if err != nil || v.YourRole != RoleViewer {
		t.Errorf("viewer GetServer role = %q, %v", v.YourRole, err)
	}
}

// A team makes a viewer admin of its groups, and of nothing else.
func TestTeamGrantsGroupAdmin(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	addTeam(t, s, f.org, "web team", []int64{f.vic.UserID}, []int64{f.g.ID})

	// On G and its servers.
	if _, err := s.StartRun(ctx, f.vic, RunRequest{Kind: RunMeasure, GroupID: &f.g.ID}); err != nil {
		t.Errorf("StartRun on G: %v", err)
	}
	if _, err := s.StartRun(ctx, f.vic, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.gSrv.ID}}); err != nil {
		t.Errorf("StartRun on G's server: %v", err)
	}
	s.Wait()
	if n := auditCount(t, s, f.org, "run.start", 0); n != 2 {
		t.Errorf("run.start audits = %d, want 2", n)
	}
	wantAllowed(t, "ServiceAction on G's server", s.ServiceAction(ctx, f.vic, f.gSrv.ID, "nginx.service", "restart"))
	wantAllowed(t, "ContainerAction on G's server", s.ContainerAction(ctx, f.vic, f.gSrv.ID, "app", "restart", ""))
	_, err := s.Logs(ctx, f.vic, f.gSrv.ID, LogQuery{Unit: "ssh.service"})
	wantAllowed(t, "Logs on G's server", err)
	_, err = s.CheckUpdates(ctx, f.vic, f.gSrv.ID)
	wantAllowed(t, "CheckUpdates on G's server", err)
	if _, err := s.UpdateGroup(ctx, f.vic, f.g.ID, testGroup("web", "Europe/London")); err != nil {
		t.Errorf("UpdateGroup G: %v", err)
	}
	if n := auditCount(t, s, f.org, "group.update", 0); n != 1 {
		t.Errorf("group.update audits = %d, want 1", n)
	}

	// Not on H.
	_, err = s.StartRun(ctx, f.vic, RunRequest{Kind: RunMeasure, GroupID: &f.h.ID})
	wantErr(t, "StartRun on H", err, ErrForbidden)
	_, err = s.StartRun(ctx, f.vic, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.hSrv.ID}})
	wantErr(t, "StartRun on H's server", err, ErrForbidden)
	_, err = s.StartRun(ctx, f.vic, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.gSrv.ID, f.hSrv.ID}})
	wantErr(t, "StartRun on G's and H's servers", err, ErrForbidden)
	wantErr(t, "ServiceAction on H's server", s.ServiceAction(ctx, f.vic, f.hSrv.ID, "nginx.service", "restart"), ErrForbidden)
	_, err = s.Logs(ctx, f.vic, f.hSrv.ID, LogQuery{Unit: "ssh.service"})
	wantErr(t, "Logs on H's server", err, ErrForbidden)
	_, err = s.UpdateGroup(ctx, f.vic, f.h.ID, testGroup("db", "UTC"))
	wantErr(t, "UpdateGroup H", err, ErrForbidden)

	// Organization-level work stays with org admins, even on G.
	_, err = s.CreateGroup(ctx, f.vic, testGroup("new", "UTC"))
	wantErr(t, "CreateGroup", err, ErrForbidden)
	wantErr(t, "DeleteGroup G", s.DeleteGroup(ctx, f.vic, f.g.ID), ErrForbidden)
	_, err = s.UpdateServer(ctx, f.vic, f.gSrv.ID, UpdateServerInput{Name: ptr("x")})
	wantErr(t, "UpdateServer", err, ErrForbidden)
	wantErr(t, "DeleteServer", s.DeleteServer(ctx, f.vic, f.gSrv.ID, false), ErrForbidden)
	_, err = s.AcceptHostKey(ctx, f.vic, f.gSrv.ID, "SHA256:x")
	wantErr(t, "AcceptHostKey", err, ErrForbidden)
	_, err = s.ReinstallHelper(ctx, f.vic, f.gSrv.ID, nil)
	wantErr(t, "ReinstallHelper", err, ErrForbidden)
	_, err = s.NewEnrollToken(ctx, f.vic, f.gSrv.ID)
	wantErr(t, "NewEnrollToken", err, ErrForbidden)
	_, err = s.ListGroupShares(ctx, f.vic, f.g.ID)
	wantErr(t, "ListGroupShares", err, ErrForbidden)
	_, err = s.CreateShareLink(ctx, f.vic, f.g.ID, "operate")
	wantErr(t, "CreateShareLink", err, ErrForbidden)

	// A token without the actions scope is refused even with team admin.
	tok := f.vic
	tok.TokenID, tok.Source, tok.Scopes = 1, SourceAPI, []Scope{ScopeRead}
	_, err = s.StartRun(ctx, tok, RunRequest{Kind: RunMeasure, GroupID: &f.g.ID})
	wantErr(t, "read token StartRun", err, ErrForbidden)
	wantErr(t, "read token ServiceAction", s.ServiceAction(ctx, tok, f.gSrv.ID, "nginx.service", "restart"), ErrForbidden)
	_, err = s.Logs(ctx, tok, f.gSrv.ID, LogQuery{Unit: "ssh.service"})
	wantErr(t, "read token Logs", err, ErrForbidden)
	_, err = s.UpdateGroup(ctx, tok, f.g.ID, testGroup("web", "UTC"))
	wantErr(t, "read token UpdateGroup", err, ErrForbidden)
	tok.Scopes = []Scope{ScopeRead, ScopeActions}
	if _, err := s.StartRun(ctx, tok, RunRequest{Kind: RunMeasure, GroupID: &f.g.ID}); err != nil {
		t.Errorf("actions token StartRun on G: %v", err)
	}
	_, err = s.StartRun(ctx, tok, RunRequest{Kind: RunMeasure, GroupID: &f.h.ID})
	wantErr(t, "actions token StartRun on H", err, ErrForbidden)
	s.Wait()

	// Leaving the team ends the right at once.
	if err := s.db.Where("user_id = ?", f.vic.UserID).Delete(&store.TeamMember{}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = s.StartRun(ctx, f.vic, RunRequest{Kind: RunMeasure, GroupID: &f.g.ID})
	wantErr(t, "StartRun after leaving the team", err, ErrForbidden)
	_, err = s.UpdateGroup(ctx, f.vic, f.g.ID, testGroup("web", "UTC"))
	wantErr(t, "UpdateGroup after leaving the team", err, ErrForbidden)
	wantErr(t, "ServiceAction after leaving the team", s.ServiceAction(ctx, f.vic, f.gSrv.ID, "nginx.service", "restart"), ErrForbidden)
}

// yourRole in server and group views.
func TestYourRole(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	admin := shareMember(t, s, f.org, signIn(t, s, "ada"), RoleAdmin)
	addTeam(t, s, f.org, "web team", []int64{f.vic.UserID, admin.UserID}, []int64{f.g.ID})

	cases := []struct {
		who    string
		a      Actor
		g, h   Role
		groups map[int64]Role
	}{
		{"owner", f.owner, RoleOwner, RoleOwner, nil},
		{"admin", admin, RoleAdmin, RoleAdmin, nil},
		{"viewer in a team", f.vic, RoleAdmin, RoleViewer, nil},
	}
	for _, c := range cases {
		gv, err := s.GetServer(ctx, c.a, f.gSrv.ID)
		if err != nil || gv.YourRole != c.g {
			t.Errorf("%s GetServer G = %q, %v; want %q", c.who, gv.YourRole, err, c.g)
		}
		hv, err := s.GetServer(ctx, c.a, f.hSrv.ID)
		if err != nil || hv.YourRole != c.h {
			t.Errorf("%s GetServer H = %q, %v; want %q", c.who, hv.YourRole, err, c.h)
		}
		ov, err := s.Overview(ctx, c.a)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range ov.Servers {
			want := map[int64]Role{f.gSrv.ID: c.g, f.hSrv.ID: c.h}[v.ID]
			if v.YourRole != want {
				t.Errorf("%s Overview %s = %q, want %q", c.who, v.Name, v.YourRole, want)
			}
		}
		groups, err := s.ListGroups(ctx, c.a)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range groups {
			want := map[int64]Role{f.g.ID: c.g, f.h.ID: c.h}[v.ID]
			if v.YourRole != want {
				t.Errorf("%s ListGroups %s = %q, want %q", c.who, v.Name, v.YourRole, want)
			}
		}
		g, err := s.GetGroup(ctx, c.a, f.g.ID)
		if err != nil || g.YourRole != c.g {
			t.Errorf("%s GetGroup G = %q, %v; want %q", c.who, g.YourRole, err, c.g)
		}
	}
}

// A team can hold a group another organization shares in. Operate lets the
// team's viewers act there; view never does.
func TestTeamOnSharedGroup(t *testing.T) {
	f := newShareFixture(t)
	s, ctx := f.s, context.Background()
	f.accept(t, "operate")
	addTeam(t, s, f.teamB, "shared ops", []int64{f.carol.UserID}, []int64{f.group.ID})

	if _, err := s.StartRun(ctx, f.carol, RunRequest{Kind: RunMeasure, GroupID: &f.group.ID}); err != nil {
		t.Errorf("team viewer StartRun on an operate share: %v", err)
	}
	if _, err := s.StartRun(ctx, f.carol, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.shared.ID}}); err != nil {
		t.Errorf("team viewer StartRun on a shared server: %v", err)
	}
	s.Wait()
	if n := auditCount(t, s, f.teamA, "run.start", f.teamB); n != 2 {
		t.Errorf("owner run.start audits via team b = %d, want 2", n)
	}
	wantAllowed(t, "team viewer ServiceAction on a shared server", s.ServiceAction(ctx, f.carol, f.shared.ID, "nginx.service", "restart"))
	_, err := s.Logs(ctx, f.carol, f.shared.ID, LogQuery{Unit: "ssh.service"})
	wantAllowed(t, "team viewer Logs on a shared server", err)
	// Settings of a shared group stay with the owner.
	_, err = s.UpdateGroup(ctx, f.carol, f.group.ID, testGroup("prod", "UTC"))
	wantErr(t, "team viewer UpdateGroup on a shared group", err, ErrNotFound)
	_, err = s.StartRun(ctx, f.carol, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.unshared.ID}})
	wantErr(t, "team viewer StartRun on an unshared server", err, ErrNotFound)

	v, err := s.GetServer(ctx, f.carol, f.shared.ID)
	if err != nil || v.YourRole != RoleAdmin {
		t.Errorf("team viewer role on an operate share = %q, %v", v.YourRole, err)
	}
	gv, err := s.GetGroup(ctx, f.bob, f.group.ID)
	if err != nil || gv.YourRole != RoleAdmin {
		t.Errorf("admin role on an operate share = %q, %v", gv.YourRole, err)
	}

	if _, err := s.UpdateGroupShare(ctx, f.alice, f.share.ID, "view"); err != nil {
		t.Fatal(err)
	}
	_, err = s.StartRun(ctx, f.carol, RunRequest{Kind: RunMeasure, GroupID: &f.group.ID})
	wantErr(t, "team viewer StartRun on a view share", err, ErrForbidden)
	wantErr(t, "team viewer ServiceAction on a view share", s.ServiceAction(ctx, f.carol, f.shared.ID, "nginx.service", "restart"), ErrForbidden)
	v, err = s.GetServer(ctx, f.carol, f.shared.ID)
	if err != nil || v.YourRole != RoleViewer {
		t.Errorf("team viewer role on a view share = %q, %v", v.YourRole, err)
	}
	groups, err := s.ListGroups(ctx, f.bob)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if g.ID == f.group.ID && g.YourRole != RoleViewer {
			t.Errorf("admin ListGroups role on a view share = %q", g.YourRole)
		}
	}
}

// Which scopes a member may put on a token.
func TestCreateTokenScopesByRole(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	admin := shareMember(t, s, f.org, signIn(t, s, "ada"), RoleAdmin)
	try := func(a Actor, sc Scope) error {
		_, _, err := s.CreateToken(ctx, a, "t", []Scope{sc}, 0)
		return err
	}
	for _, sc := range AllScopes {
		if err := try(admin, sc); err != nil {
			t.Errorf("org admin %q: %v", sc, err)
		}
	}
	if err := try(f.vic, ScopeRead); err != nil {
		t.Errorf("viewer read: %v", err)
	}
	for _, sc := range []Scope{ScopeLogs, ScopeActions, ScopeAdmin} {
		wantInvalid(t, "viewer without a team "+string(sc), try(f.vic, sc))
	}
	addTeam(t, s, f.org, "web team", []int64{f.vic.UserID}, []int64{f.g.ID})
	for _, sc := range []Scope{ScopeLogs, ScopeActions} {
		if err := try(f.vic, sc); err != nil {
			t.Errorf("viewer in a team %q: %v", sc, err)
		}
	}
	wantInvalid(t, "viewer in a team admin", try(f.vic, ScopeAdmin))
	if _, _, err := s.CreateToken(ctx, f.vic, "t", []Scope{ScopeLogs, ScopeActions}, 0); err != nil {
		t.Errorf("viewer in a team logs and actions: %v", err)
	}
}
