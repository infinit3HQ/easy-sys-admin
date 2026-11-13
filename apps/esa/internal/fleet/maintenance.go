package fleet

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// MaintenanceView is one window that quiets ordinary notifications.
type MaintenanceView struct {
	ID       int64
	GroupID  *int64
	ServerID *int64
	Reason   string
	StartsAt time.Time
	EndsAt   time.Time
}

// CreateMaintenance quiets ordinary notices for at most 7 days.
// Host key changes and new SSH keys are still sent.
func (s *Service) CreateMaintenance(ctx context.Context, a Actor, groupID, serverID *int64, reason string, start, end time.Time) (MaintenanceView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return MaintenanceView{}, err
	}
	reason = strings.Join(strings.Fields(reason), " ")
	if reason == "" || utf8.RuneCountInString(reason) > 200 {
		return MaintenanceView{}, invalid("reason", "say why, in at most 200 characters")
	}
	if !end.After(start) || end.Sub(start) > 7*24*time.Hour {
		return MaintenanceView{}, invalid("endsAt", "must be after the start and within 7 days")
	}
	if serverID != nil {
		if _, err := s.loadServer(ctx, a, *serverID); err != nil {
			return MaintenanceView{}, err
		}
	}
	if groupID != nil {
		if err := s.checkGroup(ctx, a, groupID); err != nil {
			return MaintenanceView{}, err
		}
	}
	row := store.MaintenanceWindow{
		AccountID: a.AccountID, GroupID: groupID, ServerID: serverID, Reason: reason,
		StartsAt: start, EndsAt: end, CreatedBy: a.UserID,
	}
	if err := s.db.WithContext(ctx).Omit("Account").Create(&row).Error; err != nil {
		return MaintenanceView{}, err
	}
	s.audit(ctx, nil, a, "maintenance.create", reason, map[string]any{"starts": start, "ends": end})
	return MaintenanceView{ID: row.ID, GroupID: groupID, ServerID: serverID, Reason: reason, StartsAt: start, EndsAt: end}, nil
}

// ListMaintenance returns windows for the actor's space that have not ended.
func (s *Service) ListMaintenance(ctx context.Context, a Actor) ([]MaintenanceView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	var rows []store.MaintenanceWindow
	if err := s.db.WithContext(ctx).Where("account_id = ? AND ends_at > ?", a.AccountID, s.now()).Order("starts_at").Limit(200).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]MaintenanceView, 0, len(rows))
	for _, r := range rows {
		out = append(out, MaintenanceView{ID: r.ID, GroupID: r.GroupID, ServerID: r.ServerID, Reason: r.Reason, StartsAt: r.StartsAt, EndsAt: r.EndsAt})
	}
	return out, nil
}

// DeleteMaintenance removes a window owned by the actor's space.
func (s *Service) DeleteMaintenance(ctx context.Context, a Actor, id int64) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Where("id = ? AND account_id = ?", id, a.AccountID).Delete(&store.MaintenanceWindow{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	s.audit(ctx, nil, a, "maintenance.delete", "", map[string]any{"id": id})
	return nil
}
