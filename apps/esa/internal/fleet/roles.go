package fleet

import "context"

// teamGroupsSQL selects the server groups a user is admin of through the
// teams of an organization. A team can hold the organization's own groups
// and groups another organization shares with it; visibility still decides
// whether a shared group's servers can be reached at all.
const teamGroupsSQL = `SELECT tg.group_id FROM team_groups tg
	JOIN teams t ON t.id = tg.team_id
	JOIN team_members tm ON tm.team_id = t.id
	WHERE t.account_id = ? AND tm.user_id = ?`

// roleCache resolves an actor's effective role on server groups. It loads
// the groups the actor's teams hold at most once, so list endpoints can ask
// for every row without a query each.
type roleCache struct {
	s      *Service
	a      Actor
	groups map[int64]bool // nil until loaded
}

func (s *Service) roles(a Actor) *roleCache { return &roleCache{s: s, a: a} }

// teamGroupIDs returns the ids of groups the actor's teams in their
// organization hold.
func (s *Service) teamGroupIDs(ctx context.Context, a Actor) (map[int64]bool, error) {
	var ids []int64
	if err := s.db.WithContext(ctx).Raw(teamGroupsSQL, a.AccountID, a.UserID).Scan(&ids).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// inAnyTeam reports whether the actor is in at least one team of their
// organization.
func (s *Service) inAnyTeam(ctx context.Context, a Actor) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Raw(`SELECT count(*) FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE t.account_id = ? AND tm.user_id = ?`, a.AccountID, a.UserID).Scan(&n).Error
	return n > 0, err
}

// on is the actor's role on things in groupID: their organization role,
// raised from viewer to admin when one of their teams holds the group.
// Owners and admins keep their role everywhere. Scheduled work is owner.
func (r *roleCache) on(ctx context.Context, groupID *int64) (Role, error) {
	a := r.a
	if a.Source == SourceSystem {
		return RoleOwner, nil
	}
	if a.Role != RoleViewer || groupID == nil || a.UserID == 0 {
		return a.Role, nil
	}
	if r.groups == nil {
		ids, err := r.s.teamGroupIDs(ctx, a)
		if err != nil {
			return "", err
		}
		r.groups = ids
	}
	if r.groups[*groupID] {
		return RoleAdmin, nil
	}
	return RoleViewer, nil
}

// forAccess is the actor's role on a group or a server in groupID that they
// reach with acc. A view share is viewer at most. An operate share is the
// effective role capped at admin.
func (r *roleCache) forAccess(ctx context.Context, acc Access, groupID *int64) (Role, error) {
	switch acc {
	case AccessOwn:
		return r.on(ctx, groupID)
	case AccessOperate:
		role, err := r.on(ctx, groupID)
		return minRole(role, RoleAdmin), err
	case AccessView:
		return minRole(r.a.Role, RoleViewer), nil
	}
	return "", nil
}

// canOperate checks an action on a group, or on a server in groupID. On own
// resources the effective role must be at least ownMin; on an operate share
// it must be admin. A view share never acts. The scope applies in every case.
func (r *roleCache) canOperate(ctx context.Context, acc Access, groupID *int64, ownMin Role, scope Scope) error {
	switch acc {
	case AccessOwn, AccessOperate:
		role, err := r.on(ctx, groupID)
		if err != nil {
			return err
		}
		if acc == AccessOperate {
			ownMin = RoleAdmin
		}
		return canAct(r.a, role, ownMin, scope)
	case AccessView:
		return ErrForbidden
	}
	return ErrNotFound
}

// roleOn is the actor's effective role on things in groupID. Use it for
// anything group- or server-scoped instead of a.Role.
func (s *Service) roleOn(ctx context.Context, a Actor, groupID *int64) (Role, error) {
	return s.roles(a).on(ctx, groupID)
}

// canAct is a.can with an effective role in place of the organization role.
func canAct(a Actor, role Role, min Role, scope Scope) error {
	if !role.AtLeast(min) || !a.hasScope(scope) {
		return ErrForbidden
	}
	return nil
}

func minRole(x, y Role) Role {
	if x.rank() <= y.rank() {
		return x
	}
	return y
}
