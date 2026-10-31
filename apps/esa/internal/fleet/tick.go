package fleet

import (
	"context"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// missedAfter is how late a slot may still start after a control plane restart.
const missedAfter = time.Hour

// Tick starts every group run whose schedule slot is due. It runs once a
// minute. Each slot is claimed with a conditional update, so two control
// plane processes (or two ticks) never start the same slot twice.
func (s *Service) Tick(ctx context.Context) {
	var groups []store.Group
	if err := s.db.WithContext(ctx).Where("clean_enabled OR updates_enabled").Find(&groups).Error; err != nil {
		s.log.Error("load scheduled groups", "err", err)
		return
	}
	now := s.now()
	for _, g := range groups {
		if g.CleanEnabled {
			if sc, err := parseSchedule(g.CleanDays, g.CleanTime, g.Timezone); err == nil {
				s.maybeStart(ctx, g, RunClean, "clean_slot_at", g.CleanSlotAt, sc.Latest(now), now)
			}
		}
		if g.UpdatesEnabled {
			if sc, err := parseSchedule(g.UpdateDays, g.UpdateTime, g.Timezone); err == nil {
				s.maybeStart(ctx, g, RunUpdate, "update_slot_at", g.UpdateSlotAt, sc.Latest(now), now)
			}
		}
	}
}

func (s *Service) maybeStart(ctx context.Context, g store.Group, kind RunKind, column string, claimed *time.Time, slot, now time.Time) {
	if slot.IsZero() || (claimed != nil && !claimed.Before(slot)) {
		return
	}
	res := s.db.WithContext(ctx).Model(&store.Group{}).
		Where("id = ? AND ("+column+" IS NULL OR "+column+" < ?)", g.ID, slot).
		Update(column, slot)
	if res.Error != nil || res.RowsAffected != 1 {
		return // another tick claimed it, or the group changed
	}
	a := System(g.AccountID)
	if now.Sub(slot) > missedAfter {
		// The control plane was down at the slot time. Record it instead of
		// running hours late, when nobody expects it.
		var servers []store.Server
		s.db.WithContext(ctx).Where("account_id = ? AND group_id = ?", g.AccountID, g.ID).Find(&servers)
		run, err := s.createRun(ctx, a, g.AccountID, kind, string(TriggerSchedule), &g, servers, RunOptions{}, "missed")
		if err == nil {
			fin := s.now()
			s.db.WithContext(ctx).Model(&store.Run{}).Where("id = ?", run.ID).Updates(map[string]any{
				"finished_at": fin, "summary": "missed: the control plane was not running at " + slot.In(time.UTC).Format("2006-01-02 15:04 MST"),
			})
		}
		return
	}
	if _, err := s.StartRun(ctx, a, RunRequest{Kind: kind, GroupID: &g.ID}); err != nil {
		s.log.Warn("scheduled run did not start", "group", g.Name, "kind", kind, "err", err)
	}
}
