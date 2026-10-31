package fleet

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// CleanPolicy is a group's cleanup settings.
type CleanPolicy struct {
	Enabled     bool
	Days        []string
	Time        string
	BuildCache  bool
	VSCode      bool
	Journal     bool
	JournalSize string
}

// UpdatePolicy is a group's package update settings.
type UpdatePolicy struct {
	Enabled          bool
	Mode             proto.UpdateMode
	Days             []string
	Time             string
	Hold             []string
	RebootIfRequired bool
}

// GroupInput creates or replaces a group.
type GroupInput struct {
	Name     string
	Timezone string
	Clean    CleanPolicy
	Updates  UpdatePolicy
}

// GroupView is a group as the front doors show it.
type GroupView struct {
	ID          int64
	Name        string
	Timezone    string
	Clean       CleanPolicy
	Updates     UpdatePolicy
	ServerCount int
	NextClean   *time.Time
	NextUpdate  *time.Time
	LastRuns    []RunView
	CreatedAt   time.Time
}

func toGroupView(g store.Group) GroupView {
	v := GroupView{
		ID: g.ID, Name: g.Name, Timezone: g.Timezone, CreatedAt: g.CreatedAt,
		Clean: CleanPolicy{
			Enabled: g.CleanEnabled, Days: splitDays(g.CleanDays), Time: g.CleanTime,
			BuildCache: g.CleanBuildCache, VSCode: g.CleanVSCode, Journal: g.CleanJournal, JournalSize: g.JournalSize,
		},
		Updates: UpdatePolicy{
			Enabled: g.UpdatesEnabled, Mode: proto.UpdateMode(g.UpdateMode), Days: splitDays(g.UpdateDays),
			Time: g.UpdateTime, Hold: []string{}, RebootIfRequired: g.RebootIfRequired,
		},
		LastRuns: []RunView{},
	}
	_ = g.HoldPackages.Decode(&v.Updates.Hold)
	now := time.Now()
	if sc, err := parseSchedule(g.CleanDays, g.CleanTime, g.Timezone); err == nil && g.CleanEnabled {
		n := sc.Next(now)
		v.NextClean = &n
	}
	if sc, err := parseSchedule(g.UpdateDays, g.UpdateTime, g.Timezone); err == nil && g.UpdatesEnabled {
		n := sc.Next(now)
		v.NextUpdate = &n
	}
	return v
}

func splitDays(s string) []string {
	out := []string{}
	for _, d := range strings.Split(s, ",") {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// applyGroupInput validates in and copies it onto g.
func (s *Service) applyGroupInput(g *store.Group, in GroupInput) error {
	var err error
	if g.Name, err = validName("name", in.Name); err != nil {
		return err
	}
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	cs, err := parseSchedule(strings.Join(in.Clean.Days, ","), in.Clean.Time, in.Timezone)
	if err != nil {
		return err
	}
	us, err := parseSchedule(strings.Join(in.Updates.Days, ","), in.Updates.Time, in.Timezone)
	if err != nil {
		return err
	}
	if in.Clean.JournalSize == "" {
		in.Clean.JournalSize = "200M"
	}
	if !validJournalSize(in.Clean.JournalSize) {
		return invalid("journalSize", "use a size like 200M or 1G, at least 50M")
	}
	if in.Updates.Mode == "" {
		in.Updates.Mode = proto.UpdateSecurity
	}
	if in.Updates.Mode != proto.UpdateSecurity && in.Updates.Mode != proto.UpdateAll {
		return invalid("updateMode", "must be security or all")
	}
	hold := cleanList(in.Updates.Hold)
	for _, p := range hold {
		if !pkgNameRe.MatchString(p) {
			return invalid("hold", "%q is not a valid package name", p)
		}
	}
	g.Timezone = in.Timezone
	g.CleanEnabled, g.CleanDays, g.CleanTime = in.Clean.Enabled, strings.Join(cs.Days, ","), cs.Time
	g.CleanBuildCache, g.CleanVSCode, g.CleanJournal, g.JournalSize = in.Clean.BuildCache, in.Clean.VSCode, in.Clean.Journal, in.Clean.JournalSize
	g.UpdatesEnabled, g.UpdateMode, g.UpdateDays, g.UpdateTime = in.Updates.Enabled, string(in.Updates.Mode), strings.Join(us.Days, ","), us.Time
	g.HoldPackages, g.RebootIfRequired = store.MustJSON(hold), in.Updates.RebootIfRequired

	// Mark the most recent slot as taken, so saving the form never starts a
	// run (or records a "missed" one) for a time that already passed.
	now := s.now()
	cl, ul := cs.Latest(now), us.Latest(now)
	g.CleanSlotAt, g.UpdateSlotAt = &cl, &ul
	g.UpdatedAt = now
	return nil
}

// ListGroups returns the account's groups.
func (s *Service) ListGroups(ctx context.Context, a Actor) ([]GroupView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	var groups []store.Group
	if err := s.scoped(ctx, a).Order("name").Find(&groups).Error; err != nil {
		return nil, err
	}
	counts := map[int64]int{}
	var rows []struct {
		GroupID int64
		N       int
	}
	s.scoped(ctx, a).Model(&store.Server{}).Select("group_id, count(*) AS n").Where("group_id IS NOT NULL").Group("group_id").Scan(&rows)
	for _, r := range rows {
		counts[r.GroupID] = r.N
	}
	out := make([]GroupView, 0, len(groups))
	for _, g := range groups {
		v := toGroupView(g)
		v.ServerCount = counts[g.ID]
		out = append(out, v)
	}
	return out, nil
}

// GetGroup returns one group with its recent runs.
func (s *Service) GetGroup(ctx context.Context, a Actor, id int64) (GroupView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return GroupView{}, err
	}
	var g store.Group
	if err := s.scoped(ctx, a).Where("id = ?", id).Take(&g).Error; err != nil {
		return GroupView{}, mapStore(err)
	}
	v := toGroupView(g)
	var n int64
	s.scoped(ctx, a).Model(&store.Server{}).Where("group_id = ?", id).Count(&n)
	v.ServerCount = int(n)
	runs, err := s.ListRuns(ctx, a, RunFilter{GroupID: &id, Limit: 10})
	if err != nil {
		return GroupView{}, err
	}
	v.LastRuns = runs
	return v, nil
}

// CreateGroup adds a group.
func (s *Service) CreateGroup(ctx context.Context, a Actor, in GroupInput) (GroupView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return GroupView{}, err
	}
	g := store.Group{AccountID: a.AccountID, CreatedAt: s.now()}
	if err := s.applyGroupInput(&g, in); err != nil {
		return GroupView{}, err
	}
	if err := s.db.WithContext(ctx).Omit(clause.Associations).Create(&g).Error; err != nil {
		return GroupView{}, mapStore(err)
	}
	s.audit(ctx, nil, a, "group.create", g.Name, nil)
	return s.GetGroup(ctx, a, g.ID)
}

// UpdateGroup replaces a group's settings.
func (s *Service) UpdateGroup(ctx context.Context, a Actor, id int64, in GroupInput) (GroupView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return GroupView{}, err
	}
	var g store.Group
	if err := s.scoped(ctx, a).Where("id = ?", id).Take(&g).Error; err != nil {
		return GroupView{}, mapStore(err)
	}
	if err := s.applyGroupInput(&g, in); err != nil {
		return GroupView{}, err
	}
	if err := s.db.WithContext(ctx).Omit(clause.Associations).Save(&g).Error; err != nil {
		return GroupView{}, mapStore(err)
	}
	s.audit(ctx, nil, a, "group.update", g.Name, map[string]any{
		"clean_enabled": g.CleanEnabled, "updates_enabled": g.UpdatesEnabled, "update_mode": g.UpdateMode,
	})
	return s.GetGroup(ctx, a, id)
}

// DeleteGroup removes a group. Its servers stay, without a group.
func (s *Service) DeleteGroup(ctx context.Context, a Actor, id int64) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	var g store.Group
	if err := s.scoped(ctx, a).Where("id = ?", id).Take(&g).Error; err != nil {
		return mapStore(err)
	}
	if err := s.db.WithContext(ctx).Where("id = ? AND account_id = ?", id, a.AccountID).Delete(&store.Group{}).Error; err != nil {
		return err
	}
	s.audit(ctx, nil, a, "group.delete", g.Name, nil)
	return nil
}
