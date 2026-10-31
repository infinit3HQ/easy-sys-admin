package fleet

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// Access is what the actor's account may do with a group or a server.
type Access string

const (
	// AccessNone means the actor cannot see it at all.
	AccessNone Access = ""
	// AccessOwn means it belongs to the actor's account.
	AccessOwn Access = "own"
	// AccessOperate is a share that allows runs, actions, and logs.
	AccessOperate Access = "operate"
	// AccessView is a read-only share.
	AccessView Access = "view"
)

const (
	shareLinkTTL          = 7 * 24 * time.Hour
	maxSharesPerGroup     = 50
	maxShareLinksPerGroup = 50
	maxShareTokenLen      = 128
)

// Visibility. These fragments are the only place that decides what an
// account sees through shares: groups shared with it, and servers whose
// group is one of those groups and is owned by the server's own account.
// Moving a server out of the group ends the access at once.
const (
	sharedGroupIDsSQL  = `SELECT group_id FROM group_shares WHERE account_id = ?`
	sharedServerIDsSQL = `SELECT sv.id FROM servers sv
		JOIN "groups" g ON g.id = sv.group_id AND g.account_id = sv.account_id
		JOIN group_shares gs ON gs.group_id = g.id
		WHERE gs.account_id = ?`
	// sharedRunSQL matches runs that touch a server shared with the account.
	sharedRunSQL = `EXISTS (SELECT 1 FROM run_servers rs
		JOIN servers sv ON sv.id = rs.server_id AND sv.account_id = runs.account_id
		JOIN "groups" g ON g.id = sv.group_id AND g.account_id = sv.account_id
		JOIN group_shares gs ON gs.group_id = g.id AND gs.account_id = ?
		WHERE rs.run_id = runs.id)`
)

// visibleServers is a query on servers the actor can see: their account's,
// plus servers in groups shared with it.
func (s *Service) visibleServers(ctx context.Context, a Actor) *gorm.DB {
	return s.db.WithContext(ctx).Where("(servers.account_id = ? OR servers.id IN ("+sharedServerIDsSQL+"))", a.AccountID, a.AccountID)
}

// visibleGroups is a query on groups the actor can see: their account's,
// plus groups shared with it.
func (s *Service) visibleGroups(ctx context.Context, a Actor) *gorm.DB {
	return s.db.WithContext(ctx).Where(`("groups".account_id = ? OR "groups".id IN (`+sharedGroupIDsSQL+`))`, a.AccountID, a.AccountID)
}

// visibleRuns is a query on runs the actor can see: their account's, plus
// runs that touch a server shared with it.
func (s *Service) visibleRuns(ctx context.Context, a Actor) *gorm.DB {
	return s.db.WithContext(ctx).Where("(runs.account_id = ? OR "+sharedRunSQL+")", a.AccountID, a.AccountID)
}

// sharedServerIDs returns the ids of servers shared with the account.
func (s *Service) sharedServerIDs(ctx context.Context, accountID int64) (map[int64]bool, error) {
	var ids []int64
	if err := s.db.WithContext(ctx).Raw(sharedServerIDsSQL, accountID).Scan(&ids).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// receivedShares returns the shares the account received, by group id.
func (s *Service) receivedShares(ctx context.Context, accountID int64) (map[int64]store.GroupShare, error) {
	var rows []store.GroupShare
	if err := s.db.WithContext(ctx).Where("account_id = ?", accountID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]store.GroupShare, len(rows))
	for _, r := range rows {
		out[r.GroupID] = r
	}
	return out, nil
}

// serverAccess resolves what the actor's account may do with srv.
func (s *Service) serverAccess(ctx context.Context, a Actor, srv store.Server) (Access, error) {
	if srv.AccountID == a.AccountID {
		return AccessOwn, nil
	}
	if srv.GroupID == nil {
		return AccessNone, nil
	}
	var sh store.GroupShare
	err := s.db.WithContext(ctx).
		Where(`group_id = ? AND account_id = ? AND group_id IN (SELECT id FROM "groups" WHERE account_id = ?)`, *srv.GroupID, a.AccountID, srv.AccountID).
		Take(&sh).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AccessNone, nil
	}
	if err != nil {
		return AccessNone, err
	}
	return Access(sh.Access), nil
}

// groupAccess resolves what the actor's account may do with g, and the
// share that grants it when g belongs to another account.
func (s *Service) groupAccess(ctx context.Context, a Actor, g store.Group) (Access, *store.GroupShare, error) {
	if g.AccountID == a.AccountID {
		return AccessOwn, nil, nil
	}
	var sh store.GroupShare
	err := s.db.WithContext(ctx).Where("group_id = ? AND account_id = ?", g.ID, a.AccountID).Take(&sh).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AccessNone, nil, nil
	}
	if err != nil {
		return AccessNone, nil, err
	}
	return Access(sh.Access), &sh, nil
}

// canReadServer allows any visible server. The caller checks the role and
// read scope as before.
func canReadServer(acc Access) error {
	if acc == AccessNone {
		return ErrNotFound
	}
	return nil
}

// canOperateServer checks an action on a server. On own servers the actor
// needs ownMin and scope, as before sharing existed. On a shared server the
// effective permission is the lower of the actor's role and the share:
// operate shares need admin or owner plus the scope, view shares never act.
func canOperateServer(a Actor, acc Access, ownMin Role, scope Scope) error {
	switch acc {
	case AccessOwn:
		return a.can(ownMin, scope)
	case AccessOperate:
		return a.can(RoleAdmin, scope)
	case AccessView:
		return ErrForbidden
	}
	return ErrNotFound
}

// loadVisibleGroup returns a group the actor can see, its access, and the
// share when another account owns it.
func (s *Service) loadVisibleGroup(ctx context.Context, a Actor, id int64) (store.Group, Access, *store.GroupShare, error) {
	var g store.Group
	if err := s.visibleGroups(ctx, a).Where(`"groups".id = ?`, id).Take(&g).Error; err != nil {
		return g, AccessNone, nil, mapStore(err)
	}
	acc, sh, err := s.groupAccess(ctx, a, g)
	if err != nil {
		return g, AccessNone, nil, err
	}
	if acc == AccessNone {
		return g, AccessNone, nil, ErrNotFound
	}
	return g, acc, sh, nil
}

// operableServer loads a server the actor may act on (see canOperateServer).
func (s *Service) operableServer(ctx context.Context, a Actor, id int64, ownMin Role, scope Scope) (store.Server, error) {
	srv, acc, err := s.loadVisibleServer(ctx, a, id)
	if err != nil {
		return srv, err
	}
	return srv, canOperateServer(a, acc, ownMin, scope)
}

// groupServerCounts counts each group's servers. Only servers owned by the
// group's account count, the same rule visibility uses.
func (s *Service) groupServerCounts(ctx context.Context, groupIDs []int64) (map[int64]int, error) {
	counts := map[int64]int{}
	if len(groupIDs) == 0 {
		return counts, nil
	}
	var rows []struct {
		GroupID int64
		N       int
	}
	err := s.db.WithContext(ctx).Model(&store.Server{}).
		Select("servers.group_id, count(*) AS n").
		Joins(`JOIN "groups" g ON g.id = servers.group_id AND g.account_id = servers.account_id`).
		Where("servers.group_id IN ?", groupIDs).Group("servers.group_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		counts[r.GroupID] = r.N
	}
	return counts, nil
}

// accountNames returns account names by id.
func (s *Service) accountNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []store.Account
	if err := s.db.WithContext(ctx).Select("id", "name").Where("id IN ?", uniqueIDs(ids)).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out, nil
}

// auditOn writes an audit entry for something owned by ownerID. When the
// actor works from another account (through a share), the entry goes to the
// owner with via_account_id, and a copy with owner_account_id stays in the
// actor's account.
func (s *Service) auditOn(ctx context.Context, q *store.Queries, a Actor, ownerID int64, action, target string, detail map[string]any) {
	if ownerID == 0 || ownerID == a.AccountID {
		s.audit(ctx, q, a, action, target, detail)
		return
	}
	owner := a
	owner.AccountID = ownerID
	od := maps.Clone(detail)
	if od == nil {
		od = map[string]any{}
	}
	od["via_account_id"] = a.AccountID
	s.audit(ctx, q, owner, action, target, od)

	cd := maps.Clone(detail)
	if cd == nil {
		cd = map[string]any{}
	}
	cd["owner_account_id"] = ownerID
	s.audit(ctx, q, a, action, target, cd)
}

// ---- Share management ----------------------------------------------------------

// GroupShareView is one team a group is shared with.
type GroupShareView struct {
	ID        int64
	GroupID   int64
	GroupName string
	OwnerTeam string
	Team      string
	Access    Access
	CreatedAt time.Time
}

// ShareLinkView is a pending share link. The token is never stored.
type ShareLinkView struct {
	ID        int64
	Access    Access
	CreatedAt time.Time
	ExpiresAt time.Time
	CreatedBy string
}

// CreatedShareLink is a new link with its one-time URL.
type CreatedShareLink struct {
	Link ShareLinkView
	URL  string
}

// SharePreview is what a share link offers, and the caller's teams that can
// accept it.
type SharePreview struct {
	GroupName   string
	OwnerTeam   string
	Access      Access
	ServerCount int
	ExpiresAt   time.Time
	Teams       []store.Membership
}

func validShareAccess(access string) (Access, error) {
	switch acc := Access(access); acc {
	case AccessView, AccessOperate:
		return acc, nil
	}
	return AccessNone, invalid("access", "must be view or operate")
}

// ownedGroup returns a group of the actor's own account.
func (s *Service) ownedGroup(ctx context.Context, a Actor, id int64) (store.Group, error) {
	var g store.Group
	err := s.scoped(ctx, a).Where("id = ?", id).Take(&g).Error
	return g, mapStore(err)
}

func userLabel(u *store.User) string {
	if u == nil {
		return ""
	}
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

func toShareLinkView(l store.ShareLink) ShareLinkView {
	return ShareLinkView{ID: l.ID, Access: Access(l.Access), CreatedAt: l.CreatedAt, ExpiresAt: l.ExpiresAt, CreatedBy: userLabel(l.Creator)}
}

func toGroupShareView(sh store.GroupShare, g store.Group, ownerTeam, team string) GroupShareView {
	return GroupShareView{ID: sh.ID, GroupID: g.ID, GroupName: g.Name, OwnerTeam: ownerTeam, Team: team, Access: Access(sh.Access), CreatedAt: sh.CreatedAt}
}

// ListGroupShares returns the teams a group of the actor's account is shared with.
func (s *Service) ListGroupShares(ctx context.Context, a Actor, groupID int64) ([]GroupShareView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return nil, err
	}
	g, err := s.ownedGroup(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	var rows []store.GroupShare
	if err := s.db.WithContext(ctx).Preload("Account").Where("group_id = ?", g.ID).Order("created_at, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	names, err := s.accountNames(ctx, []int64{g.AccountID})
	if err != nil {
		return nil, err
	}
	out := make([]GroupShareView, 0, len(rows))
	for _, r := range rows {
		out = append(out, toGroupShareView(r, g, names[g.AccountID], r.Account.Name))
	}
	return out, nil
}

// ListShareLinks returns a group's pending share links.
func (s *Service) ListShareLinks(ctx context.Context, a Actor, groupID int64) ([]ShareLinkView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return nil, err
	}
	g, err := s.ownedGroup(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	var rows []store.ShareLink
	if err := s.db.WithContext(ctx).Preload("Creator").Where("group_id = ? AND account_id = ? AND expires_at > ?", g.ID, a.AccountID, s.now()).
		Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ShareLinkView, 0, len(rows))
	for _, r := range rows {
		out = append(out, toShareLinkView(r))
	}
	return out, nil
}

// CreateShareLink creates a one-time link that shares a group of the
// actor's account with another team. The URL is returned only here.
func (s *Service) CreateShareLink(ctx context.Context, a Actor, groupID int64, access string) (CreatedShareLink, error) {
	// API tokens cannot hand out access, the same as they cannot accept it.
	if a.TokenID != 0 {
		return CreatedShareLink{}, ErrForbidden
	}
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return CreatedShareLink{}, err
	}
	acc, err := validShareAccess(access)
	if err != nil {
		return CreatedShareLink{}, err
	}
	g, err := s.ownedGroup(ctx, a, groupID)
	if err != nil {
		return CreatedShareLink{}, err
	}
	if err := s.limitRate(a, "share.link_create"); err != nil {
		return CreatedShareLink{}, err
	}
	now := s.now()
	token, hash := randomToken("")
	link := store.ShareLink{
		GroupID: g.ID, AccountID: g.AccountID, TokenHash: hash, Access: string(acc),
		CreatedBy: store.OptionalID(a.UserID), CreatedAt: now, ExpiresAt: now.Add(shareLinkTTL),
	}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		db := q.Raw().WithContext(ctx)
		// The group lock makes concurrent creates and accepts count one at a time.
		if _, err := lockGroup(ctx, q, g.ID); err != nil {
			return err
		}
		// Expired links are useless; drop them so they do not count against the cap.
		if err := db.Where("group_id = ? AND expires_at <= ?", g.ID, now).Delete(&store.ShareLink{}).Error; err != nil {
			return err
		}
		var shares, links int64
		if err := db.Model(&store.GroupShare{}).Where("group_id = ?", g.ID).Count(&shares).Error; err != nil {
			return err
		}
		if shares >= maxSharesPerGroup {
			return &ConflictError{Message: "this group is already shared with the maximum of 50 teams; stop sharing with one first"}
		}
		if err := db.Model(&store.ShareLink{}).Where("group_id = ?", g.ID).Count(&links).Error; err != nil {
			return err
		}
		if links >= maxShareLinksPerGroup {
			return &ConflictError{Message: "this group has 50 pending share links; revoke one first"}
		}
		if err := db.Omit(clause.Associations).Create(&link).Error; err != nil {
			return err
		}
		s.audit(ctx, q, a, "share.link_create", g.Name, map[string]any{"link_id": link.ID, "access": link.Access})
		return nil
	})
	if err != nil {
		return CreatedShareLink{}, err
	}
	if a.UserID != 0 {
		if u, err := s.store.GetUser(ctx, a.UserID); err == nil {
			link.Creator = &u
		}
	}
	return CreatedShareLink{Link: toShareLinkView(link), URL: s.publicURL.JoinPath("/share", token).String()}, nil
}

// RevokeShareLink deletes a pending share link of the actor's account.
func (s *Service) RevokeShareLink(ctx context.Context, a Actor, linkID int64) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	var link store.ShareLink
	if err := s.db.WithContext(ctx).Preload("Group").Where("id = ? AND account_id = ?", linkID, a.AccountID).Take(&link).Error; err != nil {
		return mapStore(err)
	}
	return s.store.InTx(ctx, func(q *store.Queries) error {
		res := q.Raw().Where("id = ? AND account_id = ?", link.ID, a.AccountID).Delete(&store.ShareLink{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		s.audit(ctx, q, a, "share.link_revoke", link.Group.Name, map[string]any{"link_id": link.ID, "access": link.Access})
		return nil
	})
}

// UpdateGroupShare changes a share's access. Only admins of the owning
// account can.
func (s *Service) UpdateGroupShare(ctx context.Context, a Actor, shareID int64, access string) (GroupShareView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return GroupShareView{}, err
	}
	acc, err := validShareAccess(access)
	if err != nil {
		return GroupShareView{}, err
	}
	var sh store.GroupShare
	err = s.db.WithContext(ctx).Preload("Group").Preload("Account").
		Where(`id = ? AND group_id IN (SELECT id FROM "groups" WHERE account_id = ?)`, shareID, a.AccountID).Take(&sh).Error
	if err != nil {
		return GroupShareView{}, mapStore(err)
	}
	from := sh.Access
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if err := q.Raw().Model(&store.GroupShare{}).Where("id = ?", sh.ID).Update("access", string(acc)).Error; err != nil {
			return err
		}
		s.audit(ctx, q, a, "share.update", sh.Group.Name, map[string]any{
			"share_id": sh.ID, "team_id": sh.AccountID, "team": sh.Account.Name, "from": from, "access": string(acc),
		})
		return nil
	})
	if err != nil {
		return GroupShareView{}, err
	}
	sh.Access = string(acc)
	names, err := s.accountNames(ctx, []int64{sh.Group.AccountID})
	if err != nil {
		return GroupShareView{}, err
	}
	return toGroupShareView(sh, sh.Group, names[sh.Group.AccountID], sh.Account.Name), nil
}

// RemoveGroupShare ends a share. Admins of the owning account stop sharing;
// admins of the receiving account remove the group from their team.
func (s *Service) RemoveGroupShare(ctx context.Context, a Actor, shareID int64) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	var sh store.GroupShare
	if err := s.db.WithContext(ctx).Preload("Group").Preload("Account").Where("id = ?", shareID).Take(&sh).Error; err != nil {
		return mapStore(err)
	}
	ownerID := sh.Group.AccountID
	if ownerID != a.AccountID && sh.AccountID != a.AccountID {
		return ErrNotFound
	}
	return s.store.InTx(ctx, func(q *store.Queries) error {
		if err := q.Raw().Where("id = ?", sh.ID).Delete(&store.GroupShare{}).Error; err != nil {
			return err
		}
		// By the owner: one entry in the owning account. By the receiver:
		// the owner's entry carries via_account_id, and the receiver keeps a copy.
		s.auditOn(ctx, q, a, ownerID, "share.remove", sh.Group.Name, map[string]any{
			"share_id": sh.ID, "team_id": sh.AccountID, "team": sh.Account.Name, "access": sh.Access,
		})
		return nil
	})
}

// findShareLink returns an unexpired link by token.
func (s *Service) findShareLink(db *gorm.DB, token string) (store.ShareLink, error) {
	var link store.ShareLink
	if token == "" || len(token) > maxShareTokenLen {
		return link, ErrNotFound
	}
	err := db.Where("token_hash = ? AND expires_at > ?", hashToken(token), s.now()).Take(&link).Error
	return link, mapStore(err)
}

// lockGroup locks a group row FOR NO KEY UPDATE for the rest of the
// transaction, so share and link counts against its caps happen one at a time.
func lockGroup(ctx context.Context, q *store.Queries, id int64) (store.Group, error) {
	var g store.Group
	err := q.Raw().WithContext(ctx).Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Where("id = ?", id).Take(&g).Error
	return g, mapStore(err)
}

// PreviewShareLink describes a share link and lists the caller's teams that
// can accept it: they are owner or admin there, it is not a personal space,
// not the owning account, and the group is not already shared with it.
func (s *Service) PreviewShareLink(ctx context.Context, a Actor, token string) (SharePreview, error) {
	if a.UserID == 0 {
		return SharePreview{}, ErrForbidden
	}
	link, err := s.findShareLink(s.db.WithContext(ctx), token)
	if err != nil {
		return SharePreview{}, err
	}
	var g store.Group
	if err := s.db.WithContext(ctx).Where("id = ?", link.GroupID).Take(&g).Error; err != nil {
		return SharePreview{}, mapStore(err)
	}
	if ok, err := creatorHolds(ctx, s.store.Queries, link.CreatedBy, g.AccountID, RoleAdmin); err != nil {
		return SharePreview{}, err
	} else if !ok {
		return SharePreview{}, ErrNotFound
	}
	names, err := s.accountNames(ctx, []int64{g.AccountID})
	if err != nil {
		return SharePreview{}, err
	}
	counts, err := s.groupServerCounts(ctx, []int64{g.ID})
	if err != nil {
		return SharePreview{}, err
	}
	ms, err := s.store.ListMemberships(ctx, a.UserID)
	if err != nil {
		return SharePreview{}, err
	}
	var shared []int64
	if err := s.db.WithContext(ctx).Model(&store.GroupShare{}).Where("group_id = ?", g.ID).Pluck("account_id", &shared).Error; err != nil {
		return SharePreview{}, err
	}
	teams := []store.Membership{}
	for _, m := range ms {
		if !Role(m.Role).AtLeast(RoleAdmin) || m.Account.Personal() || m.Account.ID == g.AccountID || slices.Contains(shared, m.Account.ID) {
			continue
		}
		teams = append(teams, m)
	}
	return SharePreview{
		GroupName: g.Name, OwnerTeam: names[g.AccountID], Access: Access(link.Access),
		ServerCount: counts[g.ID], ExpiresAt: link.ExpiresAt, Teams: teams,
	}, nil
}

// AcceptShareLink shares the link's group with teamID. The caller must be an
// owner or admin of that team and use a browser session. Accepting uses the
// link up.
func (s *Service) AcceptShareLink(ctx context.Context, a Actor, token string, teamID int64) (GroupShareView, error) {
	if a.TokenID != 0 || a.Source != SourceWeb || a.UserID == 0 {
		return GroupShareView{}, ErrForbidden
	}
	if teamID <= 0 {
		return GroupShareView{}, invalid("teamId", "is required")
	}
	m, err := s.Membership(ctx, a.UserID, teamID)
	if err != nil {
		return GroupShareView{}, err
	}
	if !Role(m.Role).AtLeast(RoleAdmin) {
		return GroupShareView{}, ErrForbidden
	}
	if m.Account.Personal() {
		return GroupShareView{}, invalid("teamId", "a personal space cannot receive shared groups; pick a team")
	}
	// Audit entries for the receiving side are written in the chosen team.
	rcv := Actor{UserID: a.UserID, AccountID: teamID, Role: Role(m.Role), Source: a.Source}

	var view GroupShareView
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		tx := q.Raw()
		found, err := s.findShareLink(tx, token)
		if err != nil {
			return err
		}
		// Lock order: group, then link, then memberships. CreateShareLink
		// locks the group the same way before it counts.
		g, err := lockGroup(ctx, q, found.GroupID)
		if err != nil {
			return err
		}
		link, err := s.findShareLink(tx.Clauses(clause.Locking{Strength: "UPDATE"}), token)
		if err != nil {
			return err // accepted or revoked meanwhile
		}
		// The creator must still be an admin of the owning account.
		if ok, err := creatorHolds(ctx, q, link.CreatedBy, g.AccountID, RoleAdmin); err != nil {
			return err
		} else if !ok {
			return ErrNotFound
		}
		// The caller's role may have changed since the check above.
		if ok, err := memberHolds(ctx, q, a.UserID, teamID, RoleAdmin); err != nil {
			return err
		} else if !ok {
			return ErrForbidden
		}
		if g.AccountID == teamID {
			return invalid("teamId", "this team already owns the group")
		}
		var n int64
		if err := tx.Model(&store.GroupShare{}).Where("group_id = ? AND account_id = ?", g.ID, teamID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return &ConflictError{Message: "this group is already shared with " + m.Account.Name}
		}
		if err := tx.Model(&store.GroupShare{}).Where("group_id = ?", g.ID).Count(&n).Error; err != nil {
			return err
		}
		if n >= maxSharesPerGroup {
			return &ConflictError{Message: "this group is already shared with the maximum of 50 teams"}
		}
		sh := store.GroupShare{GroupID: g.ID, AccountID: teamID, Access: link.Access, CreatedBy: store.OptionalID(a.UserID), CreatedAt: s.now()}
		if err := tx.Omit(clause.Associations).Create(&sh).Error; err != nil {
			return mapStore(err)
		}
		res := tx.Where("id = ?", link.ID).Delete(&store.ShareLink{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrNotFound
		}
		s.auditOn(ctx, q, rcv, g.AccountID, "share.accept", g.Name, map[string]any{
			"share_id": sh.ID, "link_id": link.ID, "team_id": teamID, "team": m.Account.Name, "access": sh.Access,
		})
		var owner store.Account
		if err := tx.Select("id", "name").Where("id = ?", g.AccountID).Take(&owner).Error; err != nil {
			return err
		}
		view = toGroupShareView(sh, g, owner.Name, m.Account.Name)
		return nil
	})
	return view, err
}
