package fleet

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// Extra teams of an organization (docs/teams.md). A team gives its
// members admin rights on its server groups, on top of their organization
// role. The built-in admin and viewer teams are the roles themselves and
// have no rows here.
const (
	maxTeamsPerOrg   = 100
	maxGroupsPerTeam = 200
)

// TeamView is one extra team with its groups and members.
type TeamView struct {
	ID        int64
	Name      string
	Groups    []TeamGroupView
	Members   []TeamMemberView
	CreatedAt time.Time
}

// TeamGroupView is a server group a team holds. OwnerTeam is the owning
// organization's name when another organization shares the group.
type TeamGroupView struct {
	ID        int64
	Name      string
	OwnerTeam string
}

// TeamMemberView is a member of a team.
type TeamMemberView struct {
	UserID int64
	Name   string
	Email  string
}

// TeamInput creates or replaces a team's name and groups.
type TeamInput struct {
	Name     string
	GroupIDs []int64
}

// ListTeams returns the current organization's extra teams, by name. Any
// member may read them. A personal space has none.
func (s *Service) ListTeams(ctx context.Context, a Actor) ([]TeamView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	m, err := s.teamMember(ctx, s.store.Queries, a)
	if err != nil {
		return nil, err
	}
	if m.Account.Personal() {
		return []TeamView{}, nil
	}
	return loadTeams(ctx, s.db, a.AccountID, 0)
}

// CreateTeam adds a team to the current organization. Owners and admins.
func (s *Service) CreateTeam(ctx context.Context, a Actor, in TeamInput) (TeamView, error) {
	name, ids, err := validTeamInput(a, in)
	if err != nil {
		return TeamView{}, err
	}
	if err := s.limitRate(a, "team.create"); err != nil {
		return TeamView{}, err
	}
	var out TeamView
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		acc, err := s.lockOrgAdmin(ctx, q, a)
		if err != nil {
			return err
		}
		db := q.Raw().WithContext(ctx)
		var n int64
		if err := db.Model(&store.Team{}).Where("account_id = ?", acc.ID).Count(&n).Error; err != nil {
			return err
		}
		if n >= maxTeamsPerOrg {
			return &ConflictError{Message: "this organization has " + strconv.Itoa(maxTeamsPerOrg) + " teams; delete one first"}
		}
		if err := extraTeamNameFree(ctx, q, acc, name, 0); err != nil {
			return err
		}
		if err := checkTeamGroups(ctx, q, acc.ID, ids); err != nil {
			return err
		}
		t := store.Team{AccountID: acc.ID, Name: name, CreatedAt: s.now()}
		if err := db.Omit(clause.Associations).Create(&t).Error; err != nil {
			return mapStore(err)
		}
		if err := setTeamGroups(ctx, q, t.ID, ids); err != nil {
			return err
		}
		s.audit(ctx, q, a, "team.create", t.Name, map[string]any{"team_id": t.ID, "groups": len(ids)})
		out, err = loadTeam(ctx, q.Raw(), acc.ID, t.ID)
		return err
	})
	return out, err
}

// UpdateTeam renames a team and replaces its groups. Owners and admins.
func (s *Service) UpdateTeam(ctx context.Context, a Actor, teamID int64, in TeamInput) (TeamView, error) {
	name, ids, err := validTeamInput(a, in)
	if err != nil {
		return TeamView{}, err
	}
	var out TeamView
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		acc, err := s.lockOrgAdmin(ctx, q, a)
		if err != nil {
			return err
		}
		t, err := orgTeam(ctx, q, acc.ID, teamID)
		if err != nil {
			return err
		}
		if err := extraTeamNameFree(ctx, q, acc, name, t.ID); err != nil {
			return err
		}
		if err := checkTeamGroups(ctx, q, acc.ID, ids); err != nil {
			return err
		}
		db := q.Raw().WithContext(ctx)
		detail := map[string]any{"team_id": t.ID, "groups": len(ids)}
		if name != t.Name {
			if err := db.Model(&store.Team{}).Where("id = ?", t.ID).Update("name", name).Error; err != nil {
				return mapStore(err)
			}
			detail["from"] = t.Name
		}
		if err := db.Where("team_id = ?", t.ID).Delete(&store.TeamGroup{}).Error; err != nil {
			return err
		}
		if err := setTeamGroups(ctx, q, t.ID, ids); err != nil {
			return err
		}
		s.audit(ctx, q, a, "team.update", name, detail)
		out, err = loadTeam(ctx, q.Raw(), acc.ID, t.ID)
		return err
	})
	return out, err
}

// DeleteTeam deletes a team. Its members keep their organization role.
// Owners and admins.
func (s *Service) DeleteTeam(ctx context.Context, a Actor, teamID int64) error {
	if err := orgWriter(a); err != nil {
		return err
	}
	return s.store.InTx(ctx, func(q *store.Queries) error {
		acc, err := s.lockOrgAdmin(ctx, q, a)
		if err != nil {
			return err
		}
		t, err := orgTeam(ctx, q, acc.ID, teamID)
		if err != nil {
			return err
		}
		// team_members and team_groups cascade.
		if err := q.Raw().WithContext(ctx).Where("id = ?", t.ID).Delete(&store.Team{}).Error; err != nil {
			return err
		}
		s.audit(ctx, q, a, "team.delete", t.Name, map[string]any{"team_id": t.ID})
		return nil
	})
}

// AddTeamMember puts a member of the organization in a team. Adding someone
// who is already in it changes nothing. Owners and admins.
func (s *Service) AddTeamMember(ctx context.Context, a Actor, teamID, userID int64) (TeamView, error) {
	if err := orgWriter(a); err != nil {
		return TeamView{}, err
	}
	var out TeamView
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		acc, err := s.lockOrgAdmin(ctx, q, a)
		if err != nil {
			return err
		}
		t, err := orgTeam(ctx, q, acc.ID, teamID)
		if err != nil {
			return err
		}
		// The account row lock keeps the membership in place until commit;
		// RemoveMember takes the same lock.
		m, err := q.GetMembership(ctx, userID, acc.ID)
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		res := q.Raw().WithContext(ctx).Omit(clause.Associations).Clauses(clause.OnConflict{DoNothing: true}).
			Create(&store.TeamMember{TeamID: t.ID, UserID: userID, CreatedAt: s.now()})
		if res.Error != nil {
			return mapStore(res.Error)
		}
		if res.RowsAffected > 0 {
			s.audit(ctx, q, a, "team.add_member", m.User.Email, map[string]any{"team_id": t.ID, "team": t.Name, "user_id": userID})
		}
		out, err = loadTeam(ctx, q.Raw(), acc.ID, t.ID)
		return err
	})
	return out, err
}

// RemoveTeamMember takes a user out of a team. They stay a member of the
// organization with their role. Owners and admins.
func (s *Service) RemoveTeamMember(ctx context.Context, a Actor, teamID, userID int64) error {
	if err := orgWriter(a); err != nil {
		return err
	}
	return s.store.InTx(ctx, func(q *store.Queries) error {
		acc, err := s.lockOrgAdmin(ctx, q, a)
		if err != nil {
			return err
		}
		t, err := orgTeam(ctx, q, acc.ID, teamID)
		if err != nil {
			return err
		}
		res := q.Raw().WithContext(ctx).Where("team_id = ? AND user_id = ?", t.ID, userID).Delete(&store.TeamMember{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		target := strconv.FormatInt(userID, 10)
		if u, err := q.GetUser(ctx, userID); err == nil {
			target = u.Email
		}
		s.audit(ctx, q, a, "team.remove_member", target, map[string]any{"team_id": t.ID, "team": t.Name, "user_id": userID})
		return nil
	})
}

// ---- Helpers ----------------------------------------------------------------

// orgWriter refuses actors that may not change an organization's members or
// teams: API tokens, and anyone below admin or without the admin scope.
func orgWriter(a Actor) error {
	if a.TokenID != 0 {
		return ErrForbidden
	}
	return a.can(RoleAdmin, ScopeAdmin)
}

// lockOrgAdmin locks the actor's current account for the transaction and
// checks they are still an owner or admin of it and that it is an
// organization, not a personal space.
func (s *Service) lockOrgAdmin(ctx context.Context, q *store.Queries, a Actor) (store.Account, error) {
	acc, err := lockAccount(ctx, q, a.AccountID, "NO KEY UPDATE")
	if errors.Is(err, ErrNotFound) {
		return store.Account{}, ErrForbidden
	}
	if err != nil {
		return store.Account{}, err
	}
	m, err := s.teamMember(ctx, q, a)
	if err != nil {
		return store.Account{}, err
	}
	if !Role(m.Role).AtLeast(RoleAdmin) {
		return store.Account{}, ErrForbidden
	}
	if acc.Personal() {
		return store.Account{}, errPersonalPrivate
	}
	return acc, nil
}

// validTeamInput checks the actor and the input before the transaction. It
// returns the trimmed name and the group ids without duplicates.
func validTeamInput(a Actor, in TeamInput) (string, []int64, error) {
	if err := orgWriter(a); err != nil {
		return "", nil, err
	}
	name, err := validDisplayName("name", in.Name)
	if err != nil {
		return "", nil, err
	}
	ids := slices.Clone(in.GroupIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) > maxGroupsPerTeam {
		return "", nil, invalid("groupIds", "use at most %d groups", maxGroupsPerTeam)
	}
	return name, ids, nil
}

// orgTeam loads a team of the account, or ErrNotFound.
func orgTeam(ctx context.Context, q *store.Queries, accountID, teamID int64) (store.Team, error) {
	var t store.Team
	err := q.Raw().WithContext(ctx).Where("id = ? AND account_id = ?", teamID, accountID).Take(&t).Error
	return t, mapStore(err)
}

func errTeamName(name string) error {
	return &ConflictError{Message: "a team named " + strconv.Quote(name) + " already exists in this organization"}
}

// extraTeamNameFree refuses a name that a built-in team of acc or another
// extra team (other than exceptID) already uses, ignoring case.
func extraTeamNameFree(ctx context.Context, q *store.Queries, acc store.Account, name string, exceptID int64) error {
	if strings.EqualFold(name, acc.AdminTeamName) || strings.EqualFold(name, acc.ViewerTeamName) {
		return errTeamName(name)
	}
	return teamNameFree(ctx, q, acc.ID, name, exceptID)
}

// teamNameFree refuses a name that an extra team of the account (other than
// exceptID) already uses, ignoring case. The account row is locked, so the
// check holds until commit.
func teamNameFree(ctx context.Context, q *store.Queries, accountID int64, name string, exceptID int64) error {
	var n int64
	if err := q.Raw().WithContext(ctx).Model(&store.Team{}).
		Where("account_id = ? AND lower(name) = lower(?) AND id <> ?", accountID, name, exceptID).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errTeamName(name)
	}
	return nil
}

// checkTeamGroups refuses group ids the organization cannot see: a team may
// hold the organization's own groups and groups shared with it.
func checkTeamGroups(ctx context.Context, q *store.Queries, accountID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	var n int64
	if err := q.Raw().WithContext(ctx).Model(&store.Group{}).
		Where(`"groups".id IN ? AND ("groups".account_id = ? OR "groups".id IN (`+sharedGroupIDsSQL+`))`, ids, accountID, accountID).
		Count(&n).Error; err != nil {
		return err
	}
	if int(n) != len(ids) {
		return invalid("groupIds", "use groups of this organization or groups shared with it")
	}
	return nil
}

func setTeamGroups(ctx context.Context, q *store.Queries, teamID int64, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	rows := make([]store.TeamGroup, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, store.TeamGroup{TeamID: teamID, GroupID: id})
	}
	return mapStore(q.Raw().WithContext(ctx).Omit(clause.Associations).Create(&rows).Error)
}

func loadTeam(ctx context.Context, db *gorm.DB, accountID, teamID int64) (TeamView, error) {
	ts, err := loadTeams(ctx, db, accountID, teamID)
	if err != nil {
		return TeamView{}, err
	}
	if len(ts) == 0 {
		return TeamView{}, ErrNotFound
	}
	return ts[0], nil
}

// loadTeams returns the account's teams by name, or one team when teamID is
// set. Groups the organization can no longer see (a share that ended) are
// left out; members are only current members of the organization.
func loadTeams(ctx context.Context, db *gorm.DB, accountID, teamID int64) ([]TeamView, error) {
	db = db.WithContext(ctx)
	tq := db.Where("account_id = ?", accountID)
	if teamID != 0 {
		tq = tq.Where("id = ?", teamID)
	}
	var teams []store.Team
	if err := tq.Order("lower(name), id").Find(&teams).Error; err != nil {
		return nil, err
	}
	out := make([]TeamView, 0, len(teams))
	if len(teams) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(teams))
	at := make(map[int64]int, len(teams))
	for i, t := range teams {
		ids = append(ids, t.ID)
		at[t.ID] = i
		out = append(out, TeamView{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt,
			Groups: []TeamGroupView{}, Members: []TeamMemberView{}})
	}

	var groups []struct {
		TeamID    int64
		ID        int64
		Name      string
		AccountID int64
		OwnerName string
	}
	if err := db.Table("team_groups tg").
		Select(`tg.team_id, g.id, g.name, g.account_id, acc.name AS owner_name`).
		Joins(`JOIN "groups" g ON g.id = tg.group_id`).
		Joins(`JOIN accounts acc ON acc.id = g.account_id`).
		Where(`tg.team_id IN ? AND (g.account_id = ? OR g.id IN (`+sharedGroupIDsSQL+`))`, ids, accountID, accountID).
		Order("lower(g.name), g.id").Scan(&groups).Error; err != nil {
		return nil, err
	}
	for _, g := range groups {
		v := TeamGroupView{ID: g.ID, Name: g.Name}
		if g.AccountID != accountID {
			v.OwnerTeam = g.OwnerName
		}
		out[at[g.TeamID]].Groups = append(out[at[g.TeamID]].Groups, v)
	}

	var members []struct {
		TeamID int64
		UserID int64
		Name   string
		Email  string
	}
	if err := db.Table("team_members tm").
		Select("tm.team_id, u.id AS user_id, u.name, u.email").
		Joins("JOIN users u ON u.id = tm.user_id").
		Joins("JOIN memberships m ON m.user_id = tm.user_id AND m.account_id = ?", accountID).
		Where("tm.team_id IN ?", ids).
		Order("lower(u.name), lower(u.email), u.id").Scan(&members).Error; err != nil {
		return nil, err
	}
	for _, m := range members {
		out[at[m.TeamID]].Members = append(out[at[m.TeamID]].Members, TeamMemberView{UserID: m.UserID, Name: m.Name, Email: m.Email})
	}
	return out, nil
}
