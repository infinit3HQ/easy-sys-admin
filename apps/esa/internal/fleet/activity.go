package fleet

import (
	"context"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// ActivityEntry is one audit log line.
type ActivityEntry struct {
	ID        int64
	Action    string
	Target    string
	Source    string
	UserName  string
	Detail    store.JSON
	CreatedAt time.Time
}

// Activity returns the newest audit entries of the account.
func (s *Service) Activity(ctx context.Context, a Actor, limit int) ([]ActivityEntry, error) {
	if err := a.can(RoleAdmin, ScopeRead); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []struct {
		store.AuditEntry
		UserName  string
		UserEmail string
	}
	err := s.db.WithContext(ctx).Table("audit_log").
		Select("audit_log.*, users.name AS user_name, users.email AS user_email").
		Joins("LEFT JOIN users ON users.id = audit_log.user_id").
		Where("audit_log.account_id = ?", a.AccountID).
		Order("audit_log.created_at DESC, audit_log.id DESC").Limit(limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]ActivityEntry, 0, len(rows))
	for _, r := range rows {
		name := r.UserName
		if name == "" {
			name = r.UserEmail
		}
		if name == "" && r.Source == string(SourceSystem) {
			name = "esa"
		}
		out = append(out, ActivityEntry{ID: r.ID, Action: r.Action, Target: r.Target, Source: r.Source, UserName: name, Detail: store.JSON(r.Detail), CreatedAt: r.CreatedAt})
	}
	return out, nil
}
