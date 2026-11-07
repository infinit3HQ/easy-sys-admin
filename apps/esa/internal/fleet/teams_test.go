package fleet

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// inTeam returns the actor working in the team with the given membership.
func inTeam(a Actor, m store.Membership) Actor {
	a.AccountID, a.Role = m.Account.ID, Role(m.Role)
	return a
}

func inviteToken(t *testing.T, url string) string {
	t.Helper()
	tok, ok := strings.CutPrefix(url, "http://localhost:3000/invite/")
	if !ok || tok == "" {
		t.Fatalf("invite url = %q", url)
	}
	return tok
}

// joinTeam invites the user to the team with role and accepts as them.
func joinTeam(t *testing.T, s *Service, owner, user Actor, role Role) store.Membership {
	t.Helper()
	ctx := context.Background()
	_, url, err := s.CreateInvite(ctx, owner, role, "")
	if err != nil {
		t.Fatal(err)
	}
	m, already, err := s.AcceptInvite(ctx, user, inviteToken(t, url))
	if err != nil || already {
		t.Fatalf("accept: %v, already=%v", err, already)
	}
	return m
}

func TestPersonalSpace(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	alice := signIn(t, s, "alice")

	me, err := s.Me(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if !me.Current.Account.Personal() || *me.Current.Account.PersonalOf != alice.UserID || len(me.Accounts) != 1 {
		t.Fatalf("new user's account should be their personal space: %+v", me.Current.Account)
	}
	// Signing in again reuses it.
	if again := signIn(t, s, "alice"); again.AccountID != alice.AccountID {
		t.Errorf("second sign-in got account %d, want %d", again.AccountID, alice.AccountID)
	}

	if _, _, err := s.CreateInvite(ctx, alice, RoleViewer, ""); err == nil {
		t.Error("invite into a personal space must fail")
	} else {
		wantConflict(t, "invite personal", err)
	}
	wantConflict(t, "leave personal", s.RemoveMember(ctx, alice, alice.UserID))
	wantConflict(t, "delete personal", s.DeleteOrganization(ctx, alice, me.Current.Account.Name))
	_, err = s.UpdateMemberRole(ctx, alice, alice.UserID, RoleAdmin)
	wantConflict(t, "demote in personal", err)

	team, err := s.GetOrganization(ctx, alice)
	if err != nil || !team.Personal || team.MemberCount != 1 || team.Role != RoleOwner {
		t.Errorf("GetTeam personal = %+v, %v", team, err)
	}
}

// A user whose personal space is missing gets a new one on the next
// sign-in, and the session starts there.
func TestPersonalSpaceCreatedForExistingUser(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	bob := signIn(t, s, "bob")
	team, err := s.CreateOrganization(ctx, bob, "Older team")
	if err != nil {
		t.Fatal(err)
	}
	// Drop the personal space, as if it had been deleted out from under the user.
	if err := s.db.Where("id = ?", bob.AccountID).Delete(&store.Account{}).Error; err != nil {
		t.Fatal(err)
	}
	if m, err := s.FirstMembership(ctx, bob.UserID); err != nil || m.Account.ID != team.Account.ID {
		t.Fatalf("without a personal space FirstMembership = %+v, %v", m.Account, err)
	}
	again := signIn(t, s, "bob")
	if again.AccountID == team.Account.ID {
		t.Fatal("sign-in should start in a new personal space")
	}
	m, err := s.FirstMembership(ctx, bob.UserID)
	if err != nil || m.Account.ID != again.AccountID || !m.Account.Personal() {
		t.Errorf("FirstMembership = %+v, %v; want the new personal space %d", m.Account, err, again.AccountID)
	}
}

func TestBetaAllowsOneOrganization(t *testing.T) {
	s := newTestService(t)
	s.maxOrgs = 1
	ctx := context.Background()
	alice := signIn(t, s, "alice")
	if _, err := s.CreateOrganization(ctx, alice, "One"); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateOrganization(ctx, alice, "Two")
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("second organization: %v", err)
	}
	me, err := s.Me(ctx, alice)
	if err != nil || me.MaxOrganizations != 1 {
		t.Fatalf("Me limits = %+v, %v", me, err)
	}
}

func TestTeamInviteAccept(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	alice, bob := signIn(t, s, "alice"), signIn(t, s, "bob")

	tm, err := s.CreateOrganization(ctx, alice, "Ops crew")
	if err != nil {
		t.Fatal(err)
	}
	if tm.Account.Personal() || tm.Role != string(RoleOwner) {
		t.Fatalf("CreateTeam = %+v", tm)
	}
	owner := inTeam(alice, tm)

	inv, url, err := s.CreateInvite(ctx, owner, RoleAdmin, "")
	if err != nil {
		t.Fatal(err)
	}
	if inv.Role != RoleAdmin || inv.CreatedBy != "alice" || inv.ExpiresAt.Sub(inv.CreatedAt) != inviteTTL {
		t.Errorf("invite = %+v", inv)
	}
	token := inviteToken(t, url)
	var stored store.Invite
	s.db.Where("id = ?", inv.ID).Take(&stored)
	if string(stored.TokenHash) != string(hashToken(token)) {
		t.Error("only the token's hash is stored")
	}

	p, err := s.PreviewInvite(ctx, bob, token)
	if err != nil || p.TeamName != "Ops crew" || p.Role != RoleAdmin || p.AlreadyMember || p.InvitedBy != "alice" {
		t.Errorf("preview = %+v, %v", p, err)
	}
	if _, err := s.PreviewInvite(ctx, bob, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown token preview: %v", err)
	}

	// Tokens cannot accept invites.
	if _, _, err := s.AcceptInvite(ctx, Actor{UserID: bob.UserID, AccountID: bob.AccountID, Role: RoleOwner, Source: SourceAPI, TokenID: 1}, token); !errors.Is(err, ErrForbidden) {
		t.Errorf("token accept: %v", err)
	}

	m, already, err := s.AcceptInvite(ctx, bob, token)
	if err != nil || already || m.Account.ID != tm.Account.ID || m.Role != string(RoleAdmin) {
		t.Fatalf("accept = %+v, %v, %v", m, already, err)
	}
	if got, err := s.Membership(ctx, bob.UserID, tm.Account.ID); err != nil || got.Role != string(RoleAdmin) {
		t.Errorf("bob's membership = %+v, %v", got, err)
	}
	// The invite is used up.
	if _, _, err := s.AcceptInvite(ctx, bob, token); !errors.Is(err, ErrNotFound) {
		t.Errorf("second accept: %v", err)
	}

	// Already a member: the invite stays usable.
	_, url2, err := s.CreateInvite(ctx, owner, RoleViewer, "")
	if err != nil {
		t.Fatal(err)
	}
	if p, err := s.PreviewInvite(ctx, bob, inviteToken(t, url2)); err != nil || !p.AlreadyMember {
		t.Errorf("preview for member = %+v, %v", p, err)
	}
	m, already, err = s.AcceptInvite(ctx, bob, inviteToken(t, url2))
	if err != nil || !already || m.Role != string(RoleAdmin) {
		t.Errorf("accept as member = %+v, %v, %v", m, already, err)
	}
	if list, _ := s.ListInvites(ctx, owner); len(list) != 1 {
		t.Errorf("pending invites = %d, want 1", len(list))
	}

	members, err := s.ListMembers(ctx, inTeam(bob, m))
	if err != nil || len(members) != 2 || members[0].Role != RoleOwner || !members[1].You {
		t.Errorf("members = %+v, %v", members, err)
	}
	team, err := s.GetOrganization(ctx, owner)
	if err != nil || team.MemberCount != 2 || team.Personal {
		t.Errorf("team = %+v, %v", team, err)
	}
	// Read tokens can read the team.
	tokActor := inTeam(bob, m)
	tokActor.Scopes, tokActor.TokenID = []Scope{ScopeRead}, 1
	if _, err := s.ListMembers(ctx, tokActor); err != nil {
		t.Errorf("read token ListMembers: %v", err)
	}
	if _, err := s.CreateOrganization(ctx, tokActor, "x"); !errors.Is(err, ErrForbidden) {
		t.Errorf("token CreateTeam: %v", err)
	}

	// Renaming the organization is for owners.
	if _, err := s.UpdateOrganization(ctx, inTeam(bob, m), OrgUpdate{Name: ptr("Mine now")}); !errors.Is(err, ErrForbidden) {
		t.Errorf("admin rename: %v", err)
	}
	if v, err := s.UpdateOrganization(ctx, owner, OrgUpdate{Name: ptr("Ops crew 2")}); err != nil || v.Name != "Ops crew 2" {
		t.Errorf("rename = %+v, %v", v, err)
	}
}

func TestInviteRestrictions(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	alice, bob, carol := signIn(t, s, "alice"), signIn(t, s, "bob"), signIn(t, s, "carol")
	tm, err := s.CreateOrganization(ctx, alice, "Team")
	if err != nil {
		t.Fatal(err)
	}
	owner := inTeam(alice, tm)

	// Email-restricted: only that address can accept.
	inv, url, err := s.CreateInvite(ctx, owner, RoleViewer, "  Carol@Example.com ")
	if err != nil {
		t.Fatal(err)
	}
	if inv.Email != "carol@example.com" {
		t.Errorf("email = %q", inv.Email)
	}
	if _, _, err := s.AcceptInvite(ctx, bob, inviteToken(t, url)); err == nil {
		t.Error("bob accepted carol's invite")
	} else {
		wantConflict(t, "other email", err)
	}
	if _, _, err := s.AcceptInvite(ctx, carol, inviteToken(t, url)); err != nil {
		t.Errorf("carol accept: %v", err)
	}
	if _, _, err := s.CreateInvite(ctx, owner, RoleViewer, "not an email"); err == nil {
		t.Error("bad email accepted")
	}

	// Expired.
	inv, url, err = s.CreateInvite(ctx, owner, RoleViewer, "")
	if err != nil {
		t.Fatal(err)
	}
	s.db.Model(&store.Invite{}).Where("id = ?", inv.ID).Update("expires_at", time.Now().Add(-time.Minute))
	if _, err := s.PreviewInvite(ctx, bob, inviteToken(t, url)); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired preview: %v", err)
	}
	if _, _, err := s.AcceptInvite(ctx, bob, inviteToken(t, url)); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired accept: %v", err)
	}

	// Only owners invite owners; viewers invite nobody.
	admin := inTeam(bob, joinTeam(t, s, owner, bob, RoleAdmin))
	if _, _, err := s.CreateInvite(ctx, admin, RoleOwner, ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("admin invites owner: %v", err)
	}
	if _, _, err := s.CreateInvite(ctx, admin, RoleAdmin, ""); err != nil {
		t.Errorf("admin invites admin: %v", err)
	}
	if _, _, err := s.CreateInvite(ctx, owner, RoleOwner, ""); err != nil {
		t.Errorf("owner invites owner: %v", err)
	}
	viewer, err := s.Membership(ctx, carol.UserID, tm.Account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateInvite(ctx, inTeam(carol, viewer), RoleViewer, ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer invites: %v", err)
	}
	if _, err := s.ListInvites(ctx, inTeam(carol, viewer)); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer lists invites: %v", err)
	}

	// Revoke is scoped to the account.
	inv, _, err = s.CreateInvite(ctx, owner, RoleViewer, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeInvite(ctx, Actor{UserID: bob.UserID, AccountID: bob.AccountID, Role: RoleOwner, Source: SourceWeb}, inv.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoke from another account: %v", err)
	}
	if err := s.RevokeInvite(ctx, admin, inv.ID); err != nil {
		t.Errorf("revoke: %v", err)
	}
}

func TestMemberLastOwnerAndRemoval(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	alice, bob := signIn(t, s, "alice"), signIn(t, s, "bob")
	tm, err := s.CreateOrganization(ctx, alice, "Team")
	if err != nil {
		t.Fatal(err)
	}
	owner := inTeam(alice, tm)
	bobIn := inTeam(bob, joinTeam(t, s, owner, bob, RoleViewer))

	wantConflict(t, "last owner leaves", s.RemoveMember(ctx, owner, alice.UserID))
	_, err = s.UpdateMemberRole(ctx, owner, alice.UserID, RoleAdmin)
	wantConflict(t, "last owner demoted", err)
	if err := s.RemoveMember(ctx, bobIn, alice.UserID); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer removes owner: %v", err)
	}
	if _, err := s.UpdateMemberRole(ctx, owner, bob.UserID, "boss"); err == nil {
		t.Error("unknown role accepted")
	}

	// With a second owner, the first can step down.
	if v, err := s.UpdateMemberRole(ctx, owner, bob.UserID, RoleOwner); err != nil || v.Role != RoleOwner {
		t.Fatalf("promote = %+v, %v", v, err)
	}
	if _, err := s.UpdateMemberRole(ctx, owner, alice.UserID, RoleAdmin); err != nil {
		t.Errorf("demote with another owner: %v", err)
	}
	bobIn.Role = RoleOwner
	wantConflict(t, "bob is now last owner", s.RemoveMember(ctx, bobIn, bob.UserID))

	// Removing alice ends her access; a session of hers falls back.
	hash := []byte("alice-session")
	now := time.Now()
	if err := s.store.CreateSession(ctx, store.Session{TokenHash: hash, UserID: alice.UserID, AccountID: tm.Account.ID,
		CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveMember(ctx, bobIn, alice.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Membership(ctx, alice.UserID, tm.Account.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("removed member's Membership: %v", err)
	}
	if _, err := s.ListMembers(ctx, inTeam(alice, tm)); !errors.Is(err, ErrForbidden) {
		t.Errorf("removed member ListMembers: %v", err)
	}
	if sess, err := s.store.GetSession(ctx, hash); err != nil || sess.AccountID != alice.AccountID {
		t.Errorf("removed member's session = %+v, %v; want personal space %d", sess, err, alice.AccountID)
	}

	// A member can leave.
	carol := signIn(t, s, "carol")
	carolIn := inTeam(carol, joinTeam(t, s, bobIn, carol, RoleViewer))
	if err := s.RemoveMember(ctx, carolIn, carol.UserID); err != nil {
		t.Errorf("leave: %v", err)
	}
	if err := s.RemoveMember(ctx, bobIn, carol.UserID); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove non-member: %v", err)
	}
}

func TestDeleteTeam(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	alice, bob := signIn(t, s, "alice"), signIn(t, s, "bob")
	tm, err := s.CreateOrganization(ctx, alice, "Doomed")
	if err != nil {
		t.Fatal(err)
	}
	owner := inTeam(alice, tm)
	bobIn := inTeam(bob, joinTeam(t, s, owner, bob, RoleAdmin))

	g, err := s.CreateGroup(ctx, owner, GroupInput{Name: "prod", Timezone: "UTC",
		Clean: CleanPolicy{Days: []string{"sun"}, Time: "03:00", JournalSize: "200M"}, Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := store.Server{AccountID: tm.Account.ID, GroupID: &g.ID, Name: "s1", Host: "10.0.0.1", Port: 22, Username: "esa",
		Enrollment: "ready", ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateInvite(ctx, owner, RoleViewer, ""); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	for _, sess := range []store.Session{
		{TokenHash: []byte("alice"), UserID: alice.UserID, AccountID: tm.Account.ID},
		{TokenHash: []byte("bob"), UserID: bob.UserID, AccountID: tm.Account.ID},
		{TokenHash: []byte("bob-personal"), UserID: bob.UserID, AccountID: bob.AccountID},
	} {
		sess.CreatedAt, sess.LastSeenAt, sess.ExpiresAt = now, now, now.Add(time.Hour)
		if err := s.store.CreateSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.DeleteOrganization(ctx, bobIn, "Doomed"); !errors.Is(err, ErrForbidden) {
		t.Errorf("admin delete: %v", err)
	}
	if err := s.DeleteOrganization(ctx, owner, "doomed"); err == nil {
		t.Error("delete with wrong confirmation")
	}
	if err := s.DeleteOrganization(ctx, owner, "Doomed"); err != nil {
		t.Fatal(err)
	}

	for hash, want := range map[string]int64{"alice": alice.AccountID, "bob": bob.AccountID, "bob-personal": bob.AccountID} {
		sess, err := s.store.GetSession(ctx, []byte(hash))
		if err != nil || sess.AccountID != want {
			t.Errorf("session %s = %+v, %v; want account %d", hash, sess, err, want)
		}
	}
	count := func(model any) int64 {
		var n int64
		s.db.Model(model).Where("account_id = ?", tm.Account.ID).Count(&n)
		return n
	}
	if n := count(&store.Server{}) + count(&store.Group{}) + count(&store.Membership{}) + count(&store.Invite{}); n != 0 {
		t.Errorf("%d rows of the deleted team remain", n)
	}
	if _, err := s.Membership(ctx, alice.UserID, tm.Account.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("membership after delete: %v", err)
	}
	var audit store.AuditEntry
	if err := s.db.Where("action = ?", "org.delete").Take(&audit).Error; err != nil || audit.AccountID == nil || *audit.AccountID != alice.AccountID {
		t.Errorf("org.delete audit = %+v, %v", audit, err)
	}
}

// Invites stop working when their creator leaves or loses the role that
// allowed them, and API tokens cannot create them.
func TestInviteCreatorAuthority(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	alice, bob, carol, dave := signIn(t, s, "alice"), signIn(t, s, "bob"), signIn(t, s, "carol"), signIn(t, s, "dave")
	tm, err := s.CreateOrganization(ctx, alice, "Team")
	if err != nil {
		t.Fatal(err)
	}
	owner := inTeam(alice, tm)
	bobIn := inTeam(bob, joinTeam(t, s, owner, bob, RoleAdmin))
	pending := func(userID int64) int64 {
		var n int64
		s.db.Model(&store.Invite{}).Where("account_id = ? AND created_by = ?", tm.Account.ID, userID).Count(&n)
		return n
	}

	tok := bobIn
	tok.TokenID, tok.Source, tok.Scopes = 1, SourceAPI, []Scope{ScopeAdmin}
	if _, _, err := s.CreateInvite(ctx, tok, RoleViewer, ""); !errors.Is(err, ErrForbidden) {
		t.Errorf("token CreateInvite: %v", err)
	}

	// Demotion deletes the member's invites; promotion keeps them.
	if _, _, err := s.CreateInvite(ctx, bobIn, RoleViewer, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateMemberRole(ctx, owner, bob.UserID, RoleOwner); err != nil || pending(bob.UserID) != 1 {
		t.Fatalf("promotion: %v, %d invites", err, pending(bob.UserID))
	}
	if _, err := s.UpdateMemberRole(ctx, owner, bob.UserID, RoleViewer); err != nil || pending(bob.UserID) != 0 {
		t.Errorf("demotion: %v, %d invites left", err, pending(bob.UserID))
	}

	// Removal deletes them too.
	if _, err := s.UpdateMemberRole(ctx, owner, bob.UserID, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateInvite(ctx, bobIn, RoleViewer, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveMember(ctx, owner, bob.UserID); err != nil || pending(bob.UserID) != 0 {
		t.Errorf("removal: %v, %d invites left", err, pending(bob.UserID))
	}

	// A creator whose role dropped some other way: checked at preview and accept.
	bobIn = inTeam(bob, joinTeam(t, s, owner, bob, RoleAdmin))
	_, url, err := s.CreateInvite(ctx, bobIn, RoleViewer, "")
	if err != nil {
		t.Fatal(err)
	}
	s.db.Model(&store.Membership{}).Where("account_id = ? AND user_id = ?", tm.Account.ID, bob.UserID).Update("role", string(RoleViewer))
	if _, err := s.PreviewInvite(ctx, carol, inviteToken(t, url)); !errors.Is(err, ErrNotFound) {
		t.Errorf("preview of a demoted creator's invite: %v", err)
	}
	if _, _, err := s.AcceptInvite(ctx, carol, inviteToken(t, url)); !errors.Is(err, ErrNotFound) {
		t.Errorf("accept of a demoted creator's invite: %v", err)
	}

	// Owner invites need the creator to still be an owner.
	if _, err := s.UpdateMemberRole(ctx, owner, bob.UserID, RoleOwner); err != nil {
		t.Fatal(err)
	}
	_, url, err = s.CreateInvite(ctx, owner, RoleOwner, "")
	if err != nil {
		t.Fatal(err)
	}
	s.db.Model(&store.Membership{}).Where("account_id = ? AND user_id = ?", tm.Account.ID, alice.UserID).Update("role", string(RoleAdmin))
	if _, _, err := s.AcceptInvite(ctx, carol, inviteToken(t, url)); !errors.Is(err, ErrNotFound) {
		t.Errorf("owner invite from a creator who is now admin: %v", err)
	}
	s.db.Model(&store.Membership{}).Where("account_id = ? AND user_id = ?", tm.Account.ID, alice.UserID).Update("role", string(RoleOwner))
	if _, already, err := s.AcceptInvite(ctx, carol, inviteToken(t, url)); err != nil || already {
		t.Errorf("owner invite once the creator is owner again: %v, %v", err, already)
	}

	// A deleted creator (NULL) never works.
	inv, url, err := s.CreateInvite(ctx, owner, RoleViewer, "")
	if err != nil {
		t.Fatal(err)
	}
	s.db.Model(&store.Invite{}).Where("id = ?", inv.ID).Update("created_by", nil)
	if _, _, err := s.AcceptInvite(ctx, dave, inviteToken(t, url)); !errors.Is(err, ErrNotFound) {
		t.Errorf("invite without a creator: %v", err)
	}
}

// Email and password users cannot accept email-restricted invites, because
// their address is not verified. Open invites still work.
func TestInviteEmailLocalUser(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	// The first user can sign up while sign-up is closed.
	res, err := s.SignUpPassword(ctx, "Lou", "lou@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	lou := Actor{UserID: res.User.ID, AccountID: res.AccountID, Role: RoleOwner, Source: SourceWeb}
	alice := signIn(t, s, "alice")
	tm, err := s.CreateOrganization(ctx, alice, "Team")
	if err != nil {
		t.Fatal(err)
	}
	owner := inTeam(alice, tm)

	_, url, err := s.CreateInvite(ctx, owner, RoleViewer, "lou@example.com")
	if err != nil {
		t.Fatal(err)
	}
	var conflict *ConflictError
	if _, err := s.PreviewInvite(ctx, lou, inviteToken(t, url)); !errors.As(err, &conflict) || conflict != errInviteUnverified {
		t.Errorf("preview by a local user: %v", err)
	}
	if _, _, err := s.AcceptInvite(ctx, lou, inviteToken(t, url)); !errors.As(err, &conflict) || conflict != errInviteUnverified {
		t.Errorf("accept by a local user: %v", err)
	}
	if _, err := s.Membership(ctx, lou.UserID, tm.Account.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("local user joined: %v", err)
	}
	joinTeam(t, s, owner, lou, RoleViewer) // an open invite works
}

// Creating or joining a team gives a user without a personal space one first.
func TestTeamsEnsurePersonalSpace(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	alice, bob := signIn(t, s, "alice"), signIn(t, s, "bob")
	dropPersonal := func(a Actor) {
		t.Helper()
		if err := s.db.Where("personal_of = ?", a.UserID).Delete(&store.Account{}).Error; err != nil {
			t.Fatal(err)
		}
	}
	hasPersonal := func(a Actor) bool {
		var n int64
		s.db.Model(&store.Account{}).Where("personal_of = ?", a.UserID).Count(&n)
		return n == 1
	}

	dropPersonal(alice)
	tm, err := s.CreateOrganization(ctx, alice, "Team")
	if err != nil {
		t.Fatal(err)
	}
	if !hasPersonal(alice) {
		t.Error("CreateTeam did not create a personal space")
	}
	dropPersonal(bob)
	joinTeam(t, s, inTeam(alice, tm), bob, RoleViewer)
	if !hasPersonal(bob) {
		t.Error("AcceptInvite did not create a personal space")
	}

	// Two first logins at once both succeed and share one space.
	dropPersonal(bob)
	u, err := s.store.GetUser(ctx, bob.UserID)
	if err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			errs <- s.store.InTx(ctx, func(q *store.Queries) error {
				_, err := s.ensurePersonal(ctx, q, u, SourceWeb)
				return err
			})
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Errorf("concurrent ensurePersonal: %v", err)
		}
	}
	if !hasPersonal(bob) {
		t.Error("want exactly one personal space")
	}
}
