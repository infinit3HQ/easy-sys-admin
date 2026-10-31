package fleet

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// Team caps (docs/teams.md).
const (
	maxTeamsPerUser   = 20
	maxTeamMembers    = 100
	maxPendingInvites = 50
	inviteTTL         = 7 * 24 * time.Hour
	maxInviteTokenLen = 128
)

var (
	errPersonalPrivate = &ConflictError{Message: "your personal space is private; create a team to work with other people"}
	errPersonalDelete  = &ConflictError{Message: "your personal space cannot be deleted"}
	errPersonalLeave   = &ConflictError{Message: "you cannot leave your personal space"}
	errLastOwner       = &ConflictError{Message: "a team needs at least one owner; make another member an owner first"}
	errInviteEmail     = &ConflictError{Message: "this invite is for a different email address; sign in with the address it was sent to"}
	// Email and password accounts do not verify their address.
	errInviteUnverified = &ConflictError{Message: "this invite is for a specific address; sign in with a provider that verifies it, or ask for an open invite link"}
)

// TeamView is the caller's current account.
type TeamView struct {
	ID          int64
	Name        string
	Personal    bool
	Role        Role // the caller's role
	MemberCount int
	CreatedAt   time.Time
}

// MemberView is one member of an account.
type MemberView struct {
	UserID   int64
	Name     string
	Email    string
	Role     Role
	JoinedAt time.Time
	You      bool
}

// InviteView is a pending invite without its token.
type InviteView struct {
	ID        int64
	Role      Role
	Email     string
	CreatedAt time.Time
	ExpiresAt time.Time
	CreatedBy string
}

// InvitePreview is what an invite link offers the signed-in user.
type InvitePreview struct {
	TeamName      string
	Role          Role
	InvitedBy     string
	ExpiresAt     time.Time
	AlreadyMember bool
}

// ---- Teams ------------------------------------------------------------------

// CreateTeam creates a team with the caller as its only owner. The session
// stays where it is; the caller switches with SwitchAccount.
func (s *Service) CreateTeam(ctx context.Context, a Actor, name string) (store.Membership, error) {
	if a.TokenID != 0 || a.Source == SourceSystem || a.UserID == 0 {
		return store.Membership{}, ErrForbidden
	}
	name, err := validTeamName(name)
	if err != nil {
		return store.Membership{}, err
	}
	if err := s.limitRate(a, "team.create"); err != nil {
		return store.Membership{}, err
	}
	var out store.Membership
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		// Lock the user row so concurrent requests cannot both pass the cap.
		var u store.User
		if err := q.Raw().WithContext(ctx).Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).
			Where("id = ?", a.UserID).Take(&u).Error; err != nil {
			return mapStore(err)
		}
		// Every user has a personal space before they own a team.
		if _, err := s.ensurePersonal(ctx, q, u, a.Source); err != nil {
			return err
		}
		var n int64
		if err := q.Raw().WithContext(ctx).Model(&store.Membership{}).
			Joins("JOIN accounts acc ON acc.id = memberships.account_id").
			Where("memberships.user_id = ? AND memberships.role = ? AND acc.personal_of IS NULL", a.UserID, string(RoleOwner)).
			Count(&n).Error; err != nil {
			return err
		}
		if n >= maxTeamsPerUser {
			return &ConflictError{Message: "you already own " + strconv.Itoa(maxTeamsPerUser) + " teams; delete one first"}
		}
		now := s.now()
		acc, err := q.CreateAccount(ctx, name, now)
		if err != nil {
			return err
		}
		if err := q.AddMembership(ctx, acc.ID, a.UserID, string(RoleOwner), now); err != nil {
			return err
		}
		s.audit(ctx, q, Actor{UserID: a.UserID, AccountID: acc.ID, Role: RoleOwner, Source: a.Source},
			"team.create", acc.Name, map[string]any{"from_account_id": a.AccountID})
		out = store.Membership{AccountID: acc.ID, UserID: a.UserID, Role: string(RoleOwner), CreatedAt: now, Account: acc, User: u}
		return nil
	})
	return out, err
}

// GetTeam returns the caller's current account with its member count.
func (s *Service) GetTeam(ctx context.Context, a Actor) (TeamView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return TeamView{}, err
	}
	m, err := s.teamMember(ctx, s.store.Queries, a)
	if err != nil {
		return TeamView{}, err
	}
	return teamView(ctx, s.db, m)
}

// RenameTeam renames the current account. Owners only.
func (s *Service) RenameTeam(ctx context.Context, a Actor, name string) (TeamView, error) {
	if err := a.can(RoleOwner, ScopeAdmin); err != nil {
		return TeamView{}, err
	}
	name, err := validTeamName(name)
	if err != nil {
		return TeamView{}, err
	}
	var out TeamView
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if _, err := lockAccount(ctx, q, a.AccountID, "NO KEY UPDATE"); err != nil {
			return err
		}
		m, err := s.teamMember(ctx, q, a)
		if err != nil {
			return err
		}
		if Role(m.Role) != RoleOwner {
			return ErrForbidden
		}
		old := m.Account.Name
		if old != name {
			if err := q.Raw().WithContext(ctx).Model(&store.Account{}).Where("id = ?", m.AccountID).Update("name", name).Error; err != nil {
				return err
			}
			s.audit(ctx, q, a, "team.rename", name, map[string]any{"from": old})
		}
		m.Account.Name = name
		out, err = teamView(ctx, q.Raw(), m)
		return err
	})
	return out, err
}

// DeleteTeam deletes the current team and everything it owns. confirm must
// be the team's exact name. Sessions working in the team move to their
// user's personal space (or another membership) in the same transaction.
func (s *Service) DeleteTeam(ctx context.Context, a Actor, confirm string) error {
	if err := a.can(RoleOwner, ScopeAdmin); err != nil {
		return err
	}
	var serverIDs []int64
	var teamID int64
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		acc, err := lockAccount(ctx, q, a.AccountID, "UPDATE")
		if errors.Is(err, ErrNotFound) {
			return ErrForbidden
		}
		if err != nil {
			return err
		}
		m, err := s.teamMember(ctx, q, a)
		if err != nil {
			return err
		}
		if Role(m.Role) != RoleOwner {
			return ErrForbidden
		}
		if acc.Personal() {
			return errPersonalDelete
		}
		if confirm != acc.Name {
			return invalid("confirm", "type the team's exact name to delete it")
		}
		if err := q.Raw().WithContext(ctx).Model(&store.Server{}).Where("account_id = ?", acc.ID).Pluck("id", &serverIDs).Error; err != nil {
			return err
		}
		if err := q.MoveSessionsOff(ctx, acc.ID, 0); err != nil {
			return err
		}
		// Foreign keys cascade to everything the account owns, including its
		// audit log, so the entry below goes to the caller's next account.
		if err := q.Raw().WithContext(ctx).Where("id = ?", acc.ID).Delete(&store.Account{}).Error; err != nil {
			return err
		}
		teamID = acc.ID
		next := Actor{UserID: a.UserID, Source: a.Source, TokenID: a.TokenID}
		if ms, err := q.ListMemberships(ctx, a.UserID); err == nil {
			if fb, err := defaultMembership(ms, a.UserID); err == nil {
				next.AccountID, next.Role = fb.Account.ID, Role(fb.Role)
			}
		}
		s.audit(ctx, q, next, "team.delete", acc.Name, map[string]any{"team_id": acc.ID, "servers": len(serverIDs)})
		return nil
	})
	if err != nil {
		return err
	}
	s.forgetTeam(teamID, serverIDs)
	return nil
}

// forgetTeam drops in-memory state of a deleted account: its cached SSH
// signer and pooled connections to its servers.
func (s *Service) forgetTeam(accountID int64, serverIDs []int64) {
	s.signerMu.Lock()
	delete(s.signers, accountID)
	s.signerMu.Unlock()
	for _, id := range serverIDs {
		if s.pool != nil {
			s.pool.Forget(id)
		}
		s.clearBackoff(id)
	}
}

// ---- Members ----------------------------------------------------------------

// ListMembers returns the members of the current account, owners first.
func (s *Service) ListMembers(ctx context.Context, a Actor) ([]MemberView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	if _, err := s.teamMember(ctx, s.store.Queries, a); err != nil {
		return nil, err
	}
	return listMembers(ctx, s.db, a, 0)
}

// UpdateMemberRole changes a member's role. Owners only. The last owner
// cannot be demoted, and personal spaces have no other members to change.
func (s *Service) UpdateMemberRole(ctx context.Context, a Actor, userID int64, role Role) (MemberView, error) {
	if err := a.can(RoleOwner, ScopeAdmin); err != nil {
		return MemberView{}, err
	}
	if !validTeamRole(role) {
		return MemberView{}, invalid("role", "use owner, admin, or viewer")
	}
	var out MemberView
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		acc, ms, err := s.lockMembers(ctx, q, a)
		if err != nil {
			return err
		}
		if me, ok := findMember(ms, a.UserID); !ok || Role(me.Role) != RoleOwner {
			return ErrForbidden
		}
		if acc.Personal() {
			return errPersonalPrivate
		}
		target, ok := findMember(ms, userID)
		if !ok {
			return ErrNotFound
		}
		from := Role(target.Role)
		if from != role {
			if from == RoleOwner && countOwners(ms) <= 1 {
				return errLastOwner
			}
			if role.rank() < from.rank() {
				// Links they created must not outlive the authority they had.
				if err := s.dropLinksBy(ctx, q, acc.ID, userID); err != nil {
					return err
				}
			}
			if err := q.Raw().WithContext(ctx).Model(&store.Membership{}).
				Where("account_id = ? AND user_id = ?", acc.ID, userID).Update("role", string(role)).Error; err != nil {
				return err
			}
		}
		views, err := listMembers(ctx, q.Raw(), a, userID)
		if err != nil {
			return err
		}
		if len(views) == 0 {
			return ErrNotFound
		}
		out = views[0]
		if from != role {
			s.audit(ctx, q, a, "member.role", out.Email, map[string]any{"user_id": userID, "from": from, "to": role})
		}
		return nil
	})
	return out, err
}

// RemoveMember removes a member (owners), or lets the caller leave when
// userID is their own. The last owner cannot leave, and nobody leaves a
// personal space. The removed user's API tokens for the team are revoked
// and their sessions in it move to another account.
func (s *Service) RemoveMember(ctx context.Context, a Actor, userID int64) error {
	self := userID == a.UserID
	need := RoleOwner
	if self {
		need = RoleViewer
	}
	if err := a.can(need, ScopeAdmin); err != nil {
		return err
	}
	return s.store.InTx(ctx, func(q *store.Queries) error {
		acc, ms, err := s.lockMembers(ctx, q, a)
		if err != nil {
			return err
		}
		me, ok := findMember(ms, a.UserID)
		if !ok || (!self && Role(me.Role) != RoleOwner) {
			return ErrForbidden
		}
		if acc.Personal() {
			if self {
				return errPersonalLeave
			}
			return errPersonalPrivate
		}
		target, ok := findMember(ms, userID)
		if !ok {
			return ErrNotFound
		}
		if Role(target.Role) == RoleOwner && countOwners(ms) <= 1 {
			return &ConflictError{Message: "the last owner cannot leave; make another member an owner or delete the team"}
		}
		u, err := q.GetUser(ctx, userID)
		if err != nil {
			return mapStore(err)
		}
		db := q.Raw().WithContext(ctx)
		// Invites and share links they created go first (lock order: links,
		// then the membership, the same as AcceptShareLink).
		if err := s.dropLinksBy(ctx, q, acc.ID, userID); err != nil {
			return err
		}
		if err := db.Where("account_id = ? AND user_id = ?", acc.ID, userID).Delete(&store.Membership{}).Error; err != nil {
			return err
		}
		// Tokens already stop working without the membership; revoking them
		// keeps them dead if the user is invited back later.
		if err := db.Model(&store.APIToken{}).Where("account_id = ? AND user_id = ? AND revoked_at IS NULL", acc.ID, userID).
			Update("revoked_at", s.now()).Error; err != nil {
			return err
		}
		if err := q.MoveSessionsOff(ctx, acc.ID, userID); err != nil {
			return err
		}
		action := "member.remove"
		if self {
			action = "member.leave"
		}
		s.audit(ctx, q, a, action, u.Email, map[string]any{"user_id": userID, "role": target.Role})
		return nil
	})
}

// ---- Invites ----------------------------------------------------------------

// ListInvites returns the current team's pending invites. Owners and admins.
func (s *Service) ListInvites(ctx context.Context, a Actor) ([]InviteView, error) {
	if err := a.can(RoleAdmin, ScopeRead); err != nil {
		return nil, err
	}
	m, err := s.teamMember(ctx, s.store.Queries, a)
	if err != nil {
		return nil, err
	}
	if !Role(m.Role).AtLeast(RoleAdmin) {
		return nil, ErrForbidden
	}
	var rows []store.Invite
	if err := s.db.WithContext(ctx).Preload("Creator").
		Where("account_id = ? AND expires_at > ?", a.AccountID, s.now()).
		Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]InviteView, 0, len(rows))
	for _, r := range rows {
		out = append(out, toInviteView(r))
	}
	return out, nil
}

// CreateInvite creates a one-time invite link and returns it with its URL.
// Owners and admins; only owners invite owners. email, when set, is the only
// address that can accept.
func (s *Service) CreateInvite(ctx context.Context, a Actor, role Role, email string) (InviteView, string, error) {
	// API tokens cannot hand out access, the same as they cannot accept it.
	if a.TokenID != 0 {
		return InviteView{}, "", ErrForbidden
	}
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return InviteView{}, "", err
	}
	if !validTeamRole(role) {
		return InviteView{}, "", invalid("role", "use owner, admin, or viewer")
	}
	if role == RoleOwner && !a.Role.AtLeast(RoleOwner) {
		return InviteView{}, "", ErrForbidden
	}
	if email = strings.TrimSpace(email); email != "" {
		var err error
		if email, err = NormalizeEmail(email); err != nil {
			return InviteView{}, "", err
		}
	}
	if err := s.limitRate(a, "invite.create"); err != nil {
		return InviteView{}, "", err
	}
	token, hash := randomToken("")
	var out InviteView
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		acc, ms, err := s.lockMembers(ctx, q, a)
		if err != nil {
			return err
		}
		me, ok := findMember(ms, a.UserID)
		if !ok || !Role(me.Role).AtLeast(RoleAdmin) || (role == RoleOwner && Role(me.Role) != RoleOwner) {
			return ErrForbidden
		}
		if acc.Personal() {
			return errPersonalPrivate
		}
		now := s.now()
		db := q.Raw().WithContext(ctx)
		if err := db.Where("account_id = ? AND expires_at <= ?", acc.ID, now).Delete(&store.Invite{}).Error; err != nil {
			return err
		}
		var pending int64
		if err := db.Model(&store.Invite{}).Where("account_id = ?", acc.ID).Count(&pending).Error; err != nil {
			return err
		}
		if pending >= maxPendingInvites {
			return &ConflictError{Message: "this team has " + strconv.Itoa(maxPendingInvites) + " pending invites; revoke one first"}
		}
		if len(ms) >= maxTeamMembers {
			return &ConflictError{Message: "this team has reached the limit of " + strconv.Itoa(maxTeamMembers) + " members"}
		}
		creator, err := q.GetUser(ctx, a.UserID)
		if err != nil {
			return mapStore(err)
		}
		inv := store.Invite{AccountID: acc.ID, TokenHash: hash, Role: string(role), Email: email,
			CreatedBy: &creator.ID, CreatedAt: now, ExpiresAt: now.Add(inviteTTL)}
		if err := db.Omit(clause.Associations).Create(&inv).Error; err != nil {
			return mapStore(err)
		}
		inv.Creator = &creator
		out = toInviteView(inv)
		target := email
		if target == "" {
			target = "anyone with the link"
		}
		s.audit(ctx, q, a, "invite.create", target, map[string]any{"id": inv.ID, "role": role, "email": email})
		return nil
	})
	if err != nil {
		return InviteView{}, "", err
	}
	return out, s.publicURL.JoinPath("/invite", token).String(), nil
}

// RevokeInvite deletes a pending invite of the current team. Owners and admins.
func (s *Service) RevokeInvite(ctx context.Context, a Actor, id int64) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	m, err := s.teamMember(ctx, s.store.Queries, a)
	if err != nil {
		return err
	}
	if !Role(m.Role).AtLeast(RoleAdmin) {
		return ErrForbidden
	}
	var inv store.Invite
	res := s.db.WithContext(ctx).Clauses(clause.Returning{}).Where("id = ? AND account_id = ?", id, a.AccountID).Delete(&inv)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	s.audit(ctx, nil, a, "invite.revoke", strconv.FormatInt(id, 10), map[string]any{"role": inv.Role, "email": inv.Email})
	return nil
}

// PreviewInvite describes an invite link to the signed-in user. Unknown and
// expired links are ErrNotFound; links restricted to another email address
// are refused here already.
func (s *Service) PreviewInvite(ctx context.Context, a Actor, token string) (InvitePreview, error) {
	if a.UserID == 0 || a.Source == SourceSystem {
		return InvitePreview{}, ErrForbidden
	}
	inv, err := s.findInvite(ctx, s.db, token)
	if err != nil {
		return InvitePreview{}, err
	}
	var acc store.Account
	if err := s.db.WithContext(ctx).Where("id = ?", inv.AccountID).Take(&acc).Error; err != nil {
		return InvitePreview{}, mapStore(err)
	}
	if acc.Personal() {
		return InvitePreview{}, ErrNotFound
	}
	if ok, err := creatorHolds(ctx, s.store.Queries, inv.CreatedBy, inv.AccountID, inviteNeeds(inv)); err != nil {
		return InvitePreview{}, err
	} else if !ok {
		return InvitePreview{}, ErrNotFound
	}
	u, err := s.store.GetUser(ctx, a.UserID)
	if err != nil {
		return InvitePreview{}, mapStore(err)
	}
	if err := inviteEmailCheck(inv, u); err != nil {
		return InvitePreview{}, err
	}
	_, err = s.store.GetMembership(ctx, a.UserID, inv.AccountID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return InvitePreview{}, err
	}
	invitedBy := ""
	if inv.CreatedBy != nil {
		if c, err := s.store.GetUser(ctx, *inv.CreatedBy); err == nil {
			invitedBy = inviteUserName(c)
		}
	}
	return InvitePreview{TeamName: acc.Name, Role: Role(inv.Role), InvitedBy: invitedBy, ExpiresAt: inv.ExpiresAt, AlreadyMember: err == nil}, nil
}

// AcceptInvite adds the caller to the invite's team and deletes the invite,
// in one transaction. A caller who is already a member gets their existing
// membership back with alreadyMember set, and the invite stays usable.
// Browser sessions only.
func (s *Service) AcceptInvite(ctx context.Context, a Actor, token string) (m store.Membership, alreadyMember bool, err error) {
	if a.TokenID != 0 || a.UserID == 0 || a.Source == SourceSystem {
		return store.Membership{}, false, ErrForbidden
	}
	if ok, err := s.UserAllowed(ctx, a.UserID); err != nil {
		return store.Membership{}, false, err
	} else if !ok {
		return store.Membership{}, false, ErrForbidden
	}
	found, err := s.findInvite(ctx, s.db, token)
	if err != nil {
		return store.Membership{}, false, err
	}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		u, err := q.GetUser(ctx, a.UserID)
		if err != nil {
			return mapStore(err)
		}
		// Every user has a personal space before joining a team. This locks
		// the user row first, before the account and the invite.
		if _, err := s.ensurePersonal(ctx, q, u, a.Source); err != nil {
			return err
		}
		// Lock order: account, then invite (the same as DeleteTeam's cascade).
		acc, err := lockAccount(ctx, q, found.AccountID, "NO KEY UPDATE")
		if err != nil {
			return err
		}
		if acc.Personal() {
			return ErrNotFound
		}
		var inv store.Invite
		if err := q.Raw().WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND expires_at > ?", found.ID, s.now()).Take(&inv).Error; err != nil {
			return mapStore(err) // accepted or revoked meanwhile
		}
		// The creator must still be allowed to hand out this role.
		if ok, err := creatorHolds(ctx, q, inv.CreatedBy, acc.ID, inviteNeeds(inv)); err != nil {
			return err
		} else if !ok {
			return ErrNotFound
		}
		if err := inviteEmailCheck(inv, u); err != nil {
			return err
		}
		if existing, err := q.GetMembership(ctx, a.UserID, acc.ID); err == nil {
			m, alreadyMember = existing, true
			return nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		var n int64
		if err := q.Raw().WithContext(ctx).Model(&store.Membership{}).Where("account_id = ?", acc.ID).Count(&n).Error; err != nil {
			return err
		}
		if n >= maxTeamMembers {
			return &ConflictError{Message: "this team has reached the limit of " + strconv.Itoa(maxTeamMembers) + " members"}
		}
		now := s.now()
		if err := q.AddMembership(ctx, acc.ID, u.ID, inv.Role, now); err != nil {
			return mapStore(err)
		}
		if err := q.Raw().WithContext(ctx).Where("id = ?", inv.ID).Delete(&store.Invite{}).Error; err != nil {
			return err
		}
		s.audit(ctx, q, Actor{UserID: u.ID, AccountID: acc.ID, Role: Role(inv.Role), Source: a.Source},
			"invite.accept", u.Email, map[string]any{"invite_id": inv.ID, "role": inv.Role})
		m = store.Membership{AccountID: acc.ID, UserID: u.ID, Role: inv.Role, CreatedAt: now, Account: acc, User: u}
		return nil
	})
	if err != nil {
		return store.Membership{}, false, err
	}
	return m, alreadyMember, nil
}

// ---- Helpers ----------------------------------------------------------------

// dropLinksBy deletes the pending invites and share links that userID
// created in accountID. Called when they leave or lose a role, inside the
// caller's transaction.
func (s *Service) dropLinksBy(ctx context.Context, q *store.Queries, accountID, userID int64) error {
	db := q.Raw().WithContext(ctx)
	if err := db.Where("account_id = ? AND created_by = ?", accountID, userID).Delete(&store.Invite{}).Error; err != nil {
		return err
	}
	return db.Where("account_id = ? AND created_by = ?", accountID, userID).Delete(&store.ShareLink{}).Error
}

// creatorHolds reports whether the user who created an invite or a share
// link is still a member of accountID with at least role need. A deleted
// creator (NULL) never holds it. The membership row is locked FOR SHARE so
// a concurrent demotion waits for the caller's transaction.
func creatorHolds(ctx context.Context, q *store.Queries, creator *int64, accountID int64, need Role) (bool, error) {
	if creator == nil {
		return false, nil
	}
	return memberHolds(ctx, q, *creator, accountID, need)
}

// memberHolds reports whether userID is a member of accountID with at least
// role need, locking the membership row FOR SHARE for the transaction.
func memberHolds(ctx context.Context, q *store.Queries, userID, accountID int64, need Role) (bool, error) {
	var m store.Membership
	err := q.Raw().WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).
		Where("account_id = ? AND user_id = ?", accountID, userID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return Role(m.Role).AtLeast(need), nil
}

// inviteNeeds is the role an invite's creator must still hold for it to work.
func inviteNeeds(inv store.Invite) Role {
	if Role(inv.Role) == RoleOwner {
		return RoleOwner
	}
	return RoleAdmin
}

// teamMember returns the actor's membership in their current account, with
// the account. It returns ErrForbidden when they no longer belong to it.
func (s *Service) teamMember(ctx context.Context, q *store.Queries, a Actor) (store.Membership, error) {
	if a.UserID == 0 {
		return store.Membership{}, ErrForbidden
	}
	m, err := q.GetMembership(ctx, a.UserID, a.AccountID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Membership{}, ErrForbidden
	}
	return m, err
}

// lockAccount locks the account row for the rest of the transaction, so
// membership and invite changes in one account happen one at a time.
func lockAccount(ctx context.Context, q *store.Queries, id int64, strength string) (store.Account, error) {
	var acc store.Account
	err := q.Raw().WithContext(ctx).Clauses(clause.Locking{Strength: strength}).Where("id = ?", id).Take(&acc).Error
	return acc, mapStore(err)
}

// lockMembers locks the actor's current account and returns it with its
// memberships.
func (s *Service) lockMembers(ctx context.Context, q *store.Queries, a Actor) (store.Account, []store.Membership, error) {
	acc, err := lockAccount(ctx, q, a.AccountID, "NO KEY UPDATE")
	if errors.Is(err, ErrNotFound) {
		return store.Account{}, nil, ErrForbidden
	}
	if err != nil {
		return store.Account{}, nil, err
	}
	var ms []store.Membership
	if err := q.Raw().WithContext(ctx).Where("account_id = ?", acc.ID).Find(&ms).Error; err != nil {
		return store.Account{}, nil, err
	}
	return acc, ms, nil
}

func findMember(ms []store.Membership, userID int64) (store.Membership, bool) {
	for _, m := range ms {
		if m.UserID == userID {
			return m, true
		}
	}
	return store.Membership{}, false
}

func countOwners(ms []store.Membership) int {
	n := 0
	for _, m := range ms {
		if Role(m.Role) == RoleOwner {
			n++
		}
	}
	return n
}

func teamView(ctx context.Context, db *gorm.DB, m store.Membership) (TeamView, error) {
	var n int64
	if err := db.WithContext(ctx).Model(&store.Membership{}).Where("account_id = ?", m.AccountID).Count(&n).Error; err != nil {
		return TeamView{}, err
	}
	return TeamView{ID: m.Account.ID, Name: m.Account.Name, Personal: m.Account.Personal(), Role: Role(m.Role),
		MemberCount: int(n), CreatedAt: m.Account.CreatedAt}, nil
}

// listMembers returns the account's members, owners first; userID limits it to one.
func listMembers(ctx context.Context, db *gorm.DB, a Actor, userID int64) ([]MemberView, error) {
	q := db.WithContext(ctx).Joins("User").Where("memberships.account_id = ?", a.AccountID)
	if userID != 0 {
		q = q.Where("memberships.user_id = ?", userID)
	}
	var ms []store.Membership
	if err := q.Order("CASE memberships.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, memberships.created_at, memberships.user_id").
		Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]MemberView, 0, len(ms))
	for _, m := range ms {
		out = append(out, MemberView{UserID: m.UserID, Name: m.User.Name, Email: m.User.Email, Role: Role(m.Role),
			JoinedAt: m.CreatedAt, You: m.UserID == a.UserID})
	}
	return out, nil
}

// findInvite returns the unexpired invite for a link token, or ErrNotFound.
func (s *Service) findInvite(ctx context.Context, db *gorm.DB, token string) (store.Invite, error) {
	if token == "" || len(token) > maxInviteTokenLen {
		return store.Invite{}, ErrNotFound
	}
	var inv store.Invite
	if err := db.WithContext(ctx).Where("token_hash = ?", hashToken(token)).Take(&inv).Error; err != nil {
		return store.Invite{}, mapStore(err)
	}
	if !s.now().Before(inv.ExpiresAt) {
		return store.Invite{}, ErrNotFound
	}
	return inv, nil
}

// inviteEmailCheck refuses an invite restricted to one address unless the
// user's email comes from a provider that verifies it. Email and password
// accounts never prove they own their address, so they cannot use such
// invites at all.
func inviteEmailCheck(inv store.Invite, u store.User) error {
	switch {
	case inv.Email == "":
		return nil
	case u.Issuer == LocalIssuer:
		return errInviteUnverified
	case !strings.EqualFold(strings.TrimSpace(u.Email), inv.Email):
		return errInviteEmail
	}
	return nil
}

func toInviteView(inv store.Invite) InviteView {
	v := InviteView{ID: inv.ID, Role: Role(inv.Role), Email: inv.Email, CreatedAt: inv.CreatedAt, ExpiresAt: inv.ExpiresAt}
	if inv.Creator != nil {
		v.CreatedBy = inviteUserName(*inv.Creator)
	}
	return v
}

func inviteUserName(u store.User) string {
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

func validTeamRole(r Role) bool {
	return r == RoleOwner || r == RoleAdmin || r == RoleViewer
}

func validTeamName(name string) (string, error) {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n == 0 || n > 100 || !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", invalid("name", "use 1 to 100 characters")
	}
	return name, nil
}
