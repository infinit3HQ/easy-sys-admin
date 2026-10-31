package fleet

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// shareTeam creates a team account directly; the teams API lives elsewhere.
func shareTeam(t *testing.T, s *Service, name string) int64 {
	t.Helper()
	acc := store.Account{Name: name, CreatedAt: time.Now()}
	if err := s.db.Omit(clause.Associations).Create(&acc).Error; err != nil {
		t.Fatal(err)
	}
	return acc.ID
}

// shareMember adds user to a team with role and returns the browser actor.
func shareMember(t *testing.T, s *Service, accountID int64, user Actor, role Role) Actor {
	t.Helper()
	m := store.Membership{AccountID: accountID, UserID: user.UserID, Role: string(role), CreatedAt: time.Now()}
	if err := s.db.Omit(clause.Associations).Create(&m).Error; err != nil {
		t.Fatal(err)
	}
	return Actor{UserID: user.UserID, AccountID: accountID, Role: role, Source: SourceWeb}
}

func shareToken(t *testing.T, url string) string {
	t.Helper()
	i := strings.LastIndex(url, "/share/")
	if i < 0 {
		t.Fatalf("share URL %q", url)
	}
	return url[i+len("/share/"):]
}

func wantErr(t *testing.T, what string, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("%s: want %v, got %v", what, target, err)
	}
}

func wantInvalid(t *testing.T, what string, err error) {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Errorf("%s: want a validation error, got %v", what, err)
	}
}

func auditCount(t *testing.T, s *Service, accountID int64, action string, viaAccountID int64) int64 {
	t.Helper()
	q := s.db.Model(&store.AuditEntry{}).Where("account_id = ? AND action = ?", accountID, action)
	if viaAccountID != 0 {
		q = q.Where("(detail->>'via_account_id')::bigint = ?", viaAccountID)
	}
	var n int64
	if err := q.Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

type shareFixture struct {
	s                 *Service
	teamA, teamB      int64
	alice, bob, carol Actor // alice owns A; bob is admin and carol viewer of B
	group             GroupView
	shared, unshared  store.Server // A's servers, in and outside the group
	bobServer         store.Server // B's own server
	share             GroupShareView
}

func newShareFixture(t *testing.T) *shareFixture {
	t.Helper()
	s := newTestService(t)
	ctx := context.Background()
	f := &shareFixture{s: s}
	alice, bob, carol := signIn(t, s, "alice"), signIn(t, s, "bob"), signIn(t, s, "carol")
	f.teamA, f.teamB = shareTeam(t, s, "team a"), shareTeam(t, s, "team b")
	f.alice = shareMember(t, s, f.teamA, alice, RoleOwner)
	f.bob = shareMember(t, s, f.teamB, bob, RoleAdmin)
	f.carol = shareMember(t, s, f.teamB, carol, RoleViewer)

	g, err := s.CreateGroup(ctx, f.alice, GroupInput{Name: "prod", Timezone: "UTC",
		Clean: CleanPolicy{Days: []string{"sun"}, Time: "03:00", JournalSize: "200M"}, Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00"}})
	if err != nil {
		t.Fatal(err)
	}
	f.group = g
	f.shared = addTestServer(t, s, f.alice, &g.ID, "web-1")
	f.unshared = addTestServer(t, s, f.alice, nil, "secret-1")
	f.bobServer = addTestServer(t, s, f.bob, nil, "b-1")
	return f
}

// accept shares the group with team B at access through a new link.
func (f *shareFixture) accept(t *testing.T, access string) GroupShareView {
	t.Helper()
	ctx := context.Background()
	link, err := f.s.CreateShareLink(ctx, f.alice, f.group.ID, access)
	if err != nil {
		t.Fatal(err)
	}
	sh, err := f.s.AcceptShareLink(ctx, f.bob, shareToken(t, link.URL), f.teamB)
	if err != nil {
		t.Fatal(err)
	}
	f.share = sh
	return sh
}

// Without a share, team B sees nothing of team A.
func TestShareNoneStaysIsolated(t *testing.T) {
	f := newShareFixture(t)
	ctx := context.Background()
	_, err := f.s.GetServer(ctx, f.bob, f.shared.ID)
	wantErr(t, "GetServer", err, ErrNotFound)
	_, err = f.s.GetGroup(ctx, f.bob, f.group.ID)
	wantErr(t, "GetGroup", err, ErrNotFound)
	_, err = f.s.Metrics(ctx, f.bob, f.shared.ID, time.Hour)
	wantErr(t, "Metrics", err, ErrNotFound)
	ov, err := f.s.Overview(ctx, f.bob)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Servers) != 1 || ov.Servers[0].ID != f.bobServer.ID || ov.Totals.Groups != 0 {
		t.Errorf("team b overview = %+v", ov.Totals)
	}
	if ov.Servers[0].Access != AccessOwn || ov.Servers[0].OwnerTeam != "" {
		t.Errorf("own server view = %+v", ov.Servers[0])
	}
	groups, _ := f.s.ListGroups(ctx, f.bob)
	if len(groups) != 0 {
		t.Errorf("team b sees %d groups", len(groups))
	}
	_, err = f.s.ListGroupShares(ctx, f.bob, f.group.ID)
	wantErr(t, "ListGroupShares of another team's group", err, ErrNotFound)
	_, err = f.s.CreateShareLink(ctx, f.bob, f.group.ID, "operate")
	wantErr(t, "CreateShareLink of another team's group", err, ErrNotFound)
}

// A view share shows the group and its servers and nothing else, and allows
// no action and no owner-level change.
func TestShareView(t *testing.T) {
	f := newShareFixture(t)
	s := f.s
	ctx := context.Background()
	f.accept(t, "view")

	v, err := s.GetServer(ctx, f.bob, f.shared.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Access != AccessView || v.OwnerTeam != "team a" || v.GroupName != "prod" {
		t.Errorf("shared server view = access %q owner %q group %q", v.Access, v.OwnerTeam, v.GroupName)
	}
	_, err = s.GetServer(ctx, f.bob, f.unshared.ID)
	wantErr(t, "GetServer outside the shared group", err, ErrNotFound)

	ov, err := s.Overview(ctx, f.carol)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Servers) != 2 || ov.Totals.Groups != 1 {
		t.Errorf("team b overview: %d servers, %d groups", len(ov.Servers), ov.Totals.Groups)
	}
	groups, err := s.ListGroups(ctx, f.carol)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Access != AccessView || groups[0].OwnerTeam != "team a" ||
		groups[0].ShareID == nil || *groups[0].ShareID != f.share.ID || groups[0].ServerCount != 1 {
		t.Errorf("shared group list = %+v", groups)
	}
	own, _ := s.ListGroups(ctx, f.alice)
	if len(own) != 1 || own[0].Access != AccessOwn || own[0].ShareID != nil || own[0].OwnerTeam != "" {
		t.Errorf("owner group list = %+v", own)
	}
	if _, err := s.GetGroup(ctx, f.carol, f.group.ID); err != nil {
		t.Errorf("GetGroup on shared group: %v", err)
	}
	if _, err := s.Metrics(ctx, f.carol, f.shared.ID, time.Hour); err != nil {
		t.Errorf("Metrics on shared server: %v", err)
	}
	// Reads reach the server (it never enrolled, so the call itself fails).
	var unavail *UnavailableError
	if _, err := s.Services(ctx, f.carol, f.shared.ID); !errors.As(err, &unavail) {
		t.Errorf("Services on shared server: want unavailable, got %v", err)
	}

	// Alerts on the shared server show up for team b.
	st := setSnapshot(t, s, f.shared, 95)
	s.evaluateAlerts(ctx, f.shared, &st)
	st2 := setSnapshot(t, s, f.unshared, 95)
	s.evaluateAlerts(ctx, f.unshared, &st2)
	al, err := s.ListAlerts(ctx, f.carol, false, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(al) != 1 || al[0].ServerID == nil || *al[0].ServerID != f.shared.ID {
		t.Errorf("team b alerts = %+v", al)
	}

	_, err = s.StartRun(ctx, f.bob, RunRequest{Kind: RunMeasure, GroupID: &f.group.ID})
	wantErr(t, "StartRun on a view group", err, ErrForbidden)
	_, err = s.StartRun(ctx, f.bob, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.shared.ID}})
	wantErr(t, "StartRun on a view server", err, ErrForbidden)
	wantErr(t, "ServiceAction", s.ServiceAction(ctx, f.bob, f.shared.ID, "ssh.service", "restart"), ErrForbidden)
	_, err = s.Logs(ctx, f.bob, f.shared.ID, LogQuery{Unit: "ssh.service"})
	wantErr(t, "Logs", err, ErrForbidden)
	_, err = s.CheckUpdates(ctx, f.bob, f.shared.ID)
	wantErr(t, "CheckUpdates", err, ErrForbidden)
	_, err = s.ImageCheck(ctx, f.bob, f.shared.ID)
	wantErr(t, "ImageCheck", err, ErrForbidden)
	wantErr(t, "ContainerAction", s.ContainerAction(ctx, f.bob, f.shared.ID, "x", "restart", ""), ErrForbidden)

	_, err = s.UpdateServer(ctx, f.bob, f.shared.ID, UpdateServerInput{Name: ptr("stolen")})
	wantErr(t, "UpdateServer", err, ErrNotFound)
	wantErr(t, "DeleteServer", s.DeleteServer(ctx, f.bob, f.shared.ID, false), ErrNotFound)
	_, err = s.AcceptHostKey(ctx, f.bob, f.shared.ID, "SHA256:x")
	wantErr(t, "AcceptHostKey", err, ErrNotFound)
	_, err = s.NewEnrollToken(ctx, f.bob, f.shared.ID)
	wantErr(t, "NewEnrollToken", err, ErrNotFound)
	_, err = s.UpdateGroup(ctx, f.bob, f.group.ID, GroupInput{Name: "mine", Timezone: "UTC"})
	wantErr(t, "UpdateGroup", err, ErrNotFound)
	wantErr(t, "DeleteGroup", s.DeleteGroup(ctx, f.bob, f.group.ID), ErrNotFound)
	_, err = s.UpdateGroupShare(ctx, f.bob, f.share.ID, "operate")
	wantErr(t, "receiver raising its own access", err, ErrNotFound)

	// A server never joins a group of another account, shared or not.
	_, err = s.UpdateServer(ctx, f.bob, f.bobServer.ID, UpdateServerInput{GroupID: &f.group.ID})
	wantInvalid(t, "moving an own server into a shared group", err)

	shares, err := s.ListGroupShares(ctx, f.alice, f.group.ID)
	if err != nil || len(shares) != 1 || shares[0].Team != "team b" || shares[0].OwnerTeam != "team a" || shares[0].Access != AccessView {
		t.Errorf("owner's share list = %+v, %v", shares, err)
	}
	if n := auditCount(t, s, f.teamA, "share.accept", f.teamB); n != 1 {
		t.Errorf("owner audit share.accept = %d", n)
	}
	if n := auditCount(t, s, f.teamB, "share.accept", 0); n != 1 {
		t.Errorf("receiver audit share.accept = %d", n)
	}
}

// An operate share lets admins of the receiving team run and act; the run
// belongs to the owner and both teams see it.
func TestShareOperate(t *testing.T) {
	f := newShareFixture(t)
	s := f.s
	ctx := context.Background()
	f.accept(t, "view")
	if _, err := s.UpdateGroupShare(ctx, f.alice, f.share.ID, "operate"); err != nil {
		t.Fatal(err)
	}
	if n := auditCount(t, s, f.teamA, "share.update", 0); n != 1 {
		t.Errorf("share.update audit = %d", n)
	}

	_, err := s.StartRun(ctx, f.carol, RunRequest{Kind: RunMeasure, GroupID: &f.group.ID})
	wantErr(t, "viewer of the receiving team starting a run", err, ErrForbidden)
	_, err = s.Logs(ctx, f.carol, f.shared.ID, LogQuery{Unit: "ssh.service"})
	wantErr(t, "viewer of the receiving team reading logs", err, ErrForbidden)
	tok := f.bob
	tok.TokenID, tok.Source, tok.Scopes = 1, SourceAPI, []Scope{ScopeRead}
	_, err = s.StartRun(ctx, tok, RunRequest{Kind: RunMeasure, GroupID: &f.group.ID})
	wantErr(t, "token without actions scope", err, ErrForbidden)

	run, err := s.StartRun(ctx, f.bob, RunRequest{Kind: RunMeasure, GroupID: &f.group.ID})
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	var row store.Run
	s.db.Where("id = ?", run.ID).Take(&row)
	if row.AccountID != f.teamA || row.RequestedBy == nil || *row.RequestedBy != f.bob.UserID {
		t.Errorf("run owner = %d, requested by %v; want team a and bob", row.AccountID, row.RequestedBy)
	}
	if _, err := s.GetRun(ctx, f.alice, run.ID); err != nil {
		t.Errorf("owner GetRun: %v", err)
	}
	if _, err := s.GetRun(ctx, f.carol, run.ID); err != nil {
		t.Errorf("receiver GetRun: %v", err)
	}
	if n := auditCount(t, s, f.teamA, "run.start", f.teamB); n != 1 {
		t.Errorf("owner run.start audit with via_account_id = %d", n)
	}
	if n := auditCount(t, s, f.teamB, "run.start", 0); n != 1 {
		t.Errorf("receiver run.start audit copy = %d", n)
	}

	// Explicit servers work too, but never a mix of own and shared.
	if _, err := s.StartRun(ctx, f.bob, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.shared.ID}}); err != nil {
		t.Errorf("StartRun on a shared server: %v", err)
	}
	_, err = s.StartRun(ctx, f.bob, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.shared.ID, f.bobServer.ID}})
	wantInvalid(t, "run mixing own and shared servers", err)
	_, err = s.StartRun(ctx, f.bob, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.unshared.ID}})
	wantErr(t, "StartRun on the owner's unshared server", err, ErrNotFound)

	// The owner's runs elsewhere stay hidden; a run that touches both shows
	// team b only the shared server.
	hidden, err := s.StartRun(ctx, f.alice, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.unshared.ID}})
	if err != nil {
		t.Fatal(err)
	}
	both, err := s.StartRun(ctx, f.alice, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.unshared.ID, f.shared.ID}})
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	_, err = s.GetRun(ctx, f.bob, hidden.ID)
	wantErr(t, "receiver GetRun of an unshared run", err, ErrNotFound)
	bv, err := s.GetRun(ctx, f.bob, both.ID)
	if err != nil || len(bv.Servers) != 1 || bv.Servers[0].ServerID != f.shared.ID {
		t.Errorf("receiver view of a mixed owner run = %+v, %v", bv.Servers, err)
	}
	runs, _ := s.ListRuns(ctx, f.bob, RunFilter{})
	ids := map[int64]bool{}
	for _, r := range runs {
		ids[r.ID] = true
	}
	if !ids[run.ID] || !ids[both.ID] || ids[hidden.ID] {
		t.Errorf("receiver runs = %v", ids)
	}
	ownRuns, _ := s.ListRuns(ctx, f.alice, RunFilter{})
	if len(ownRuns) != 4 {
		t.Errorf("owner sees %d runs, want 4", len(ownRuns))
	}

	// Moving the server out of the group ends team b's access at once.
	if _, err := s.UpdateServer(ctx, f.alice, f.shared.ID, UpdateServerInput{ClearGroup: true}); err != nil {
		t.Fatal(err)
	}
	_, err = s.GetServer(ctx, f.bob, f.shared.ID)
	wantErr(t, "GetServer after moving out", err, ErrNotFound)
	_, err = s.StartRun(ctx, f.bob, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.shared.ID}})
	wantErr(t, "StartRun after moving out", err, ErrNotFound)
	_, err = s.GetRun(ctx, f.bob, run.ID)
	wantErr(t, "old run after moving out", err, ErrNotFound)
	g, err := s.GetGroup(ctx, f.bob, f.group.ID)
	if err != nil || g.ServerCount != 0 {
		t.Errorf("shared group after moving out = %d servers, %v", g.ServerCount, err)
	}
}

// Who can accept a link, and that it works once.
func TestShareAcceptRules(t *testing.T) {
	f := newShareFixture(t)
	s := f.s
	ctx := context.Background()
	teamC := shareTeam(t, s, "team c")
	shareMember(t, s, teamC, Actor{UserID: f.bob.UserID}, RoleAdmin)

	// Bob's own space is personal and cannot receive groups.
	personal := signIn(t, s, "bob")
	if personal.AccountID == f.teamB || personal.AccountID == teamC {
		t.Fatal("bob's first account should be his own")
	}
	s.db.Model(&store.Account{}).Where("id = ? AND personal_of IS NULL", personal.AccountID).Update("personal_of", f.bob.UserID)

	link, err := s.CreateShareLink(ctx, f.alice, f.group.ID, "operate")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link.URL, "http://localhost:3000/share/") || link.Link.Access != AccessOperate ||
		link.Link.ExpiresAt.Sub(link.Link.CreatedAt) != 7*24*time.Hour || link.Link.CreatedBy == "" {
		t.Errorf("link = %+v", link)
	}
	tok := shareToken(t, link.URL)
	var stored store.ShareLink
	s.db.Take(&stored, link.Link.ID)
	if string(stored.TokenHash) == tok || len(stored.TokenHash) != 32 {
		t.Error("only the SHA-256 of the token is stored")
	}
	_, err = s.CreateShareLink(ctx, f.alice, f.group.ID, "admin")
	wantInvalid(t, "bad access", err)
	_, err = s.CreateShareLink(ctx, f.bob, f.group.ID, "view")
	wantErr(t, "another team creating a link", err, ErrNotFound)
	viewerA := shareMember(t, s, f.teamA, Actor{UserID: f.carol.UserID}, RoleViewer)
	_, err = s.CreateShareLink(ctx, viewerA, f.group.ID, "view")
	wantErr(t, "viewer creating a link", err, ErrForbidden)

	p, err := s.PreviewShareLink(ctx, f.bob, tok)
	if err != nil {
		t.Fatal(err)
	}
	teams := map[int64]bool{}
	for _, m := range p.Teams {
		teams[m.Account.ID] = true
	}
	if p.GroupName != "prod" || p.OwnerTeam != "team a" || p.ServerCount != 1 || p.Access != AccessOperate ||
		!teams[f.teamB] || !teams[teamC] || teams[personal.AccountID] || len(teams) != 2 {
		t.Errorf("preview = %+v, teams %v", p, teams)
	}
	pa, err := s.PreviewShareLink(ctx, f.alice, tok)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range pa.Teams {
		if m.Account.ID == f.teamA {
			t.Error("the owning team is never offered")
		}
	}

	_, err = s.AcceptShareLink(ctx, personal, tok, personal.AccountID)
	wantInvalid(t, "accepting into a personal space", err)
	_, err = s.AcceptShareLink(ctx, f.alice, tok, f.teamA)
	wantInvalid(t, "owner accepting its own link", err)
	_, err = s.AcceptShareLink(ctx, f.carol, tok, f.teamB)
	wantErr(t, "viewer accepting", err, ErrForbidden)
	_, err = s.AcceptShareLink(ctx, f.alice, tok, f.teamB)
	wantErr(t, "non-member accepting for a team", err, ErrForbidden)
	viaToken := f.bob
	viaToken.TokenID, viaToken.Source = 7, SourceAPI
	_, err = s.AcceptShareLink(ctx, viaToken, tok, f.teamB)
	wantErr(t, "accepting with an API token", err, ErrForbidden)

	sh, err := s.AcceptShareLink(ctx, f.bob, tok, f.teamB)
	if err != nil {
		t.Fatal(err)
	}
	if sh.Access != AccessOperate || sh.Team != "team b" || sh.OwnerTeam != "team a" || sh.GroupID != f.group.ID {
		t.Errorf("share = %+v", sh)
	}
	_, err = s.AcceptShareLink(ctx, f.bob, tok, teamC)
	wantErr(t, "second use of a link", err, ErrNotFound)
	_, err = s.PreviewShareLink(ctx, f.bob, tok)
	wantErr(t, "preview of a used link", err, ErrNotFound)

	// Already shared with team b: a new link is still offered to team c only.
	link2, err := s.CreateShareLink(ctx, f.alice, f.group.ID, "view")
	if err != nil {
		t.Fatal(err)
	}
	tok2 := shareToken(t, link2.URL)
	p, _ = s.PreviewShareLink(ctx, f.bob, tok2)
	if len(p.Teams) != 1 || p.Teams[0].Account.ID != teamC {
		t.Errorf("preview after sharing = %+v", p.Teams)
	}
	var conflict *ConflictError
	if _, err := s.AcceptShareLink(ctx, f.bob, tok2, f.teamB); !errors.As(err, &conflict) {
		t.Errorf("accepting into a team that already has the group: %v", err)
	}
	links, err := s.ListShareLinks(ctx, f.alice, f.group.ID)
	if err != nil || len(links) != 1 || links[0].ID != link2.Link.ID {
		t.Errorf("pending links = %+v, %v", links, err)
	}

	// Expired links do nothing.
	s.db.Model(&store.ShareLink{}).Where("id = ?", link2.Link.ID).Update("expires_at", time.Now().Add(-time.Minute))
	_, err = s.AcceptShareLink(ctx, f.bob, tok2, teamC)
	wantErr(t, "expired link", err, ErrNotFound)
	_, err = s.PreviewShareLink(ctx, f.bob, tok2)
	wantErr(t, "preview of an expired link", err, ErrNotFound)

	// Revoke.
	link3, _ := s.CreateShareLink(ctx, f.alice, f.group.ID, "view")
	wantErr(t, "another team revoking", s.RevokeShareLink(ctx, f.bob, link3.Link.ID), ErrNotFound)
	if err := s.RevokeShareLink(ctx, f.alice, link3.Link.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.AcceptShareLink(ctx, f.bob, shareToken(t, link3.URL), teamC)
	wantErr(t, "revoked link", err, ErrNotFound)
	if n := auditCount(t, s, f.teamA, "share.link_create", 0); n != 3 {
		t.Errorf("share.link_create audit = %d", n)
	}
	if n := auditCount(t, s, f.teamA, "share.link_revoke", 0); n != 1 {
		t.Errorf("share.link_revoke audit = %d", n)
	}
}

// Admins of the receiving team can remove a shared group; others cannot.
func TestShareRemove(t *testing.T) {
	f := newShareFixture(t)
	s := f.s
	ctx := context.Background()
	f.accept(t, "operate")
	other := shareMember(t, s, shareTeam(t, s, "team d"), Actor{UserID: f.carol.UserID}, RoleOwner)

	wantErr(t, "viewer removing", s.RemoveGroupShare(ctx, f.carol, f.share.ID), ErrForbidden)
	wantErr(t, "unrelated team removing", s.RemoveGroupShare(ctx, other, f.share.ID), ErrNotFound)
	if err := s.RemoveGroupShare(ctx, f.bob, f.share.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetServer(ctx, f.bob, f.shared.ID)
	wantErr(t, "GetServer after removal", err, ErrNotFound)
	if groups, _ := s.ListGroups(ctx, f.bob); len(groups) != 0 {
		t.Errorf("team b still lists %d groups", len(groups))
	}
	if n := auditCount(t, s, f.teamA, "share.remove", f.teamB); n != 1 {
		t.Errorf("owner share.remove audit = %d", n)
	}
	if n := auditCount(t, s, f.teamB, "share.remove", 0); n != 1 {
		t.Errorf("receiver share.remove audit = %d", n)
	}

	// The owner can stop sharing too.
	f.accept(t, "view")
	if err := s.RemoveGroupShare(ctx, f.alice, f.share.ID); err != nil {
		t.Fatal(err)
	}
	if shares, _ := s.ListGroupShares(ctx, f.alice, f.group.ID); len(shares) != 0 {
		t.Errorf("shares after stop = %+v", shares)
	}
}

// Share links stop working when their creator leaves the owning team or is
// no longer an admin there. Tokens cannot create links, and long tokens are
// refused before any lookup.
func TestShareLinkCreatorAuthority(t *testing.T) {
	f := newShareFixture(t)
	s := f.s
	ctx := context.Background()
	dave := shareMember(t, s, f.teamA, signIn(t, s, "dave"), RoleAdmin)
	links := func() int64 {
		var n int64
		s.db.Model(&store.ShareLink{}).Where("account_id = ? AND created_by = ?", f.teamA, dave.UserID).Count(&n)
		return n
	}

	tok := dave
	tok.TokenID, tok.Source, tok.Scopes = 1, SourceAPI, []Scope{ScopeAdmin}
	_, err := s.CreateShareLink(ctx, tok, f.group.ID, "view")
	wantErr(t, "token CreateShareLink", err, ErrForbidden)
	_, err = s.PreviewShareLink(ctx, f.bob, strings.Repeat("x", 129))
	wantErr(t, "over-long token", err, ErrNotFound)

	// Demotion and removal delete the member's links.
	if _, err := s.CreateShareLink(ctx, dave, f.group.ID, "view"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateMemberRole(ctx, f.alice, dave.UserID, RoleViewer); err != nil || links() != 0 {
		t.Errorf("demotion: %v, %d links left", err, links())
	}
	if _, err := s.UpdateMemberRole(ctx, f.alice, dave.UserID, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateShareLink(ctx, dave, f.group.ID, "view"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveMember(ctx, f.alice, dave.UserID); err != nil || links() != 0 {
		t.Errorf("removal: %v, %d links left", err, links())
	}

	// A creator whose role dropped some other way: checked at preview and accept.
	dave = shareMember(t, s, f.teamA, Actor{UserID: dave.UserID}, RoleAdmin)
	link, err := s.CreateShareLink(ctx, dave, f.group.ID, "view")
	if err != nil {
		t.Fatal(err)
	}
	s.db.Model(&store.Membership{}).Where("account_id = ? AND user_id = ?", f.teamA, dave.UserID).Update("role", string(RoleViewer))
	_, err = s.PreviewShareLink(ctx, f.bob, shareToken(t, link.URL))
	wantErr(t, "preview of a demoted creator's link", err, ErrNotFound)
	_, err = s.AcceptShareLink(ctx, f.bob, shareToken(t, link.URL), f.teamB)
	wantErr(t, "accept of a demoted creator's link", err, ErrNotFound)

	// A deleted creator (NULL) never works.
	link, err = s.CreateShareLink(ctx, f.alice, f.group.ID, "view")
	if err != nil {
		t.Fatal(err)
	}
	s.db.Model(&store.ShareLink{}).Where("id = ?", link.Link.ID).Update("created_by", nil)
	_, err = s.AcceptShareLink(ctx, f.bob, shareToken(t, link.URL), f.teamB)
	wantErr(t, "link without a creator", err, ErrNotFound)

	// The caller's own role is checked again inside the transaction; a link
	// the owner still backs works for an admin of the receiving team.
	link, err = s.CreateShareLink(ctx, f.alice, f.group.ID, "view")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AcceptShareLink(ctx, f.bob, shareToken(t, link.URL), f.teamB); err != nil {
		t.Errorf("accept: %v", err)
	}
	var shares int64
	s.db.Model(&store.GroupShare{}).Where("group_id = ?", f.group.ID).Count(&shares)
	if shares != 1 {
		t.Errorf("shares = %d, want 1", shares)
	}
}

// Many concurrent accepts of links for one group never pass the cap.
func TestShareAcceptCapUnderConcurrency(t *testing.T) {
	f := newShareFixture(t)
	s := f.s
	ctx := context.Background()
	// Fill the group up to one below the cap with shares to throwaway teams.
	for i := range maxSharesPerGroup - 1 {
		team := shareTeam(t, s, "filler "+strconv.Itoa(i))
		if err := s.db.Omit(clause.Associations).Create(&store.GroupShare{GroupID: f.group.ID, AccountID: team, Access: "view", CreatedAt: time.Now()}).Error; err != nil {
			t.Fatal(err)
		}
	}
	const n = 4
	type try struct {
		who  Actor
		team int64
		tok  string
	}
	tries := make([]try, n)
	for i := range n {
		team := shareTeam(t, s, "taker "+strconv.Itoa(i))
		who := shareMember(t, s, team, signIn(t, s, "taker"+strconv.Itoa(i)), RoleAdmin)
		link, err := s.CreateShareLink(ctx, f.alice, f.group.ID, "view")
		if err != nil {
			t.Fatal(err)
		}
		tries[i] = try{who, team, shareToken(t, link.URL)}
	}
	errs := make(chan error, n)
	for _, tr := range tries {
		go func() {
			_, err := s.AcceptShareLink(ctx, tr.who, tr.tok, tr.team)
			errs <- err
		}()
	}
	ok := 0
	for range n {
		if err := <-errs; err == nil {
			ok++
		} else {
			wantConflict(t, "accept over the cap", err)
		}
	}
	var shares int64
	s.db.Model(&store.GroupShare{}).Where("group_id = ?", f.group.ID).Count(&shares)
	if ok != 1 || shares != maxSharesPerGroup {
		t.Errorf("%d accepts succeeded, %d shares; want 1 and %d", ok, shares, maxSharesPerGroup)
	}
}

// A receiving team sees a run's group only when that group is shared with
// it, a summary of the servers it can see, and the server filter matches
// only visible servers.
func TestShareRunMetadata(t *testing.T) {
	f := newShareFixture(t)
	s := f.s
	ctx := context.Background()
	f.accept(t, "view")
	staging, err := s.CreateGroup(ctx, f.alice, GroupInput{Name: "staging", Timezone: "UTC",
		Clean: CleanPolicy{Days: []string{"sun"}, Time: "03:00", JournalSize: "200M"}, Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00"}})
	if err != nil {
		t.Fatal(err)
	}
	moved := addTestServer(t, s, f.alice, &staging.ID, "stage-1")
	addTestServer(t, s, f.alice, &staging.ID, "stage-2")

	// A run on the private group, then one of its servers joins the shared group.
	run, err := s.StartRun(ctx, f.alice, RunRequest{Kind: RunMeasure, GroupID: &staging.ID})
	if err != nil {
		t.Fatal(err)
	}
	both, err := s.StartRun(ctx, f.alice, RunRequest{Kind: RunMeasure, ServerIDs: []int64{f.unshared.ID, f.shared.ID}})
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if _, err := s.UpdateServer(ctx, f.alice, moved.ID, UpdateServerInput{GroupID: &f.group.ID}); err != nil {
		t.Fatal(err)
	}

	own, err := s.GetRun(ctx, f.alice, run.ID)
	if err != nil || own.GroupName != "staging" || len(own.Servers) != 2 {
		t.Fatalf("owner view = %+v, %v", own, err)
	}
	got, err := s.GetRun(ctx, f.bob, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.GroupID != nil || got.GroupName != "" {
		t.Errorf("receiver sees the private group: %v %q", got.GroupID, got.GroupName)
	}
	if len(got.Servers) != 1 || got.Servers[0].ServerID != moved.ID {
		t.Errorf("receiver servers = %+v", got.Servers)
	}
	wantSummary := func(v RunView) string {
		rs := make([]store.RunServer, 0, len(v.Servers))
		for _, x := range v.Servers {
			rs = append(rs, store.RunServer{Status: x.Status, BytesFreed: x.BytesFreed})
		}
		_, _, _, sum := runTally(v.Kind, rs)
		return sum
	}
	if got.Summary == "" || got.Summary != wantSummary(got) || got.Summary == own.Summary {
		t.Errorf("receiver summary %q, owner %q; want %q", got.Summary, own.Summary, wantSummary(got))
	}

	// The shared group stays visible on runs of the shared group.
	gr, err := s.StartRun(ctx, f.alice, RunRequest{Kind: RunMeasure, GroupID: &f.group.ID})
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if v, err := s.GetRun(ctx, f.bob, gr.ID); err != nil || v.GroupID == nil || *v.GroupID != f.group.ID || v.GroupName != "prod" {
		t.Errorf("receiver view of a shared-group run = %v %q, %v", v.GroupID, v.GroupName, err)
	}

	ids := func(a Actor, flt RunFilter) map[int64]bool {
		t.Helper()
		runs, err := s.ListRuns(ctx, a, flt)
		if err != nil {
			t.Fatal(err)
		}
		out := map[int64]bool{}
		for _, r := range runs {
			out[r.ID] = true
		}
		return out
	}
	if got := ids(f.bob, RunFilter{ServerID: &f.unshared.ID}); len(got) != 0 {
		t.Errorf("filter on a private server matched runs %v", got)
	}
	if got := ids(f.bob, RunFilter{ServerID: &f.shared.ID}); !got[both.ID] {
		t.Errorf("filter on a shared server = %v, want run %d", got, both.ID)
	}
	if got := ids(f.bob, RunFilter{GroupID: &staging.ID}); len(got) != 0 {
		t.Errorf("filter on a private group matched runs %v", got)
	}
	if got := ids(f.bob, RunFilter{GroupID: &f.group.ID}); !got[gr.ID] || len(got) != 1 {
		t.Errorf("filter on the shared group = %v, want only run %d", got, gr.ID)
	}
	if got := ids(f.alice, RunFilter{ServerID: &f.unshared.ID}); !got[both.ID] {
		t.Errorf("owner filter on own server = %v", got)
	}
}
