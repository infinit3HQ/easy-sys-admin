package fleet

import (
	"context"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// DeliveryView is one notification send, as admins see it.
type DeliveryView struct {
	ID        int64
	Channel   string
	Title     string
	Severity  string
	State     string
	Attempts  int
	LastError string
	CreatedAt time.Time
	SentAt    *time.Time
}

// ListDeliveries returns the space's 100 most recent sends. failedOnly
// keeps the ones that gave up (state dead).
func (s *Service) ListDeliveries(ctx context.Context, a Actor, failedOnly bool) ([]DeliveryView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return nil, err
	}
	q := s.db.WithContext(ctx).Preload("Channel").Where("account_id = ?", a.AccountID).Order("id desc").Limit(100)
	if failedOnly {
		q = q.Where("state = ?", "dead")
	}
	var rows []store.NotifyDelivery
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]DeliveryView, 0, len(rows))
	for _, r := range rows {
		out = append(out, DeliveryView{
			ID: r.ID, Channel: r.Channel.Name, Title: r.Title, Severity: r.Severity, State: r.State,
			Attempts: r.Attempts, LastError: r.LastError, CreatedAt: r.CreatedAt, SentAt: r.SentAt,
		})
	}
	return out, nil
}

// RetryDelivery queues a send that gave up, with a fresh attempt count.
// A send that may have reached the channel is not retried: only dead ones.
func (s *Service) RetryDelivery(ctx context.Context, a Actor, id int64) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Model(&store.NotifyDelivery{}).
		Where("id = ? AND account_id = ? AND state = ?", id, a.AccountID, "dead").
		Updates(map[string]any{"state": "queued", "attempts": 0, "claimed_at": nil})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var n int64
		s.db.WithContext(ctx).Model(&store.NotifyDelivery{}).Where("id = ? AND account_id = ?", id, a.AccountID).Count(&n)
		if n == 0 {
			return ErrNotFound
		}
		return invalid("state", "only a delivery that gave up can be retried")
	}
	s.audit(ctx, nil, a, "delivery.retry", "", map[string]any{"id": id})
	s.kickNotify(id)
	return nil
}
