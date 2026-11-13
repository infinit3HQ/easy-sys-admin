package fleet

import (
	"context"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// PruneAudit deletes audit rows older than the install retention. Zero
// retention keeps every row. The ledger of this prune is a different table,
// and the audit row for the prune is written after the delete.
func (s *Service) PruneAudit(ctx context.Context) {
	if s.auditRetention <= 0 {
		return
	}
	if !s.auditHoldUntil.IsZero() && s.now().Before(s.auditHoldUntil) {
		s.log.Info("audit prune skipped", "hold_until", s.auditHoldUntil.UTC().Format(time.RFC3339))
		return
	}
	cutoff := s.now().Add(-s.auditRetention)
	eligible := s.countOldAudit(ctx, cutoff)
	var deleted int64
	for deleted < 100000 {
		res := s.db.WithContext(ctx).Exec(
			`DELETE FROM audit_log WHERE id IN (SELECT id FROM audit_log WHERE created_at < ? ORDER BY created_at LIMIT 1000)`,
			cutoff,
		)
		if res.Error != nil || res.RowsAffected == 0 {
			break
		}
		deleted += res.RowsAffected
	}
	_ = s.db.WithContext(ctx).Create(&store.RetentionRun{
		Kind: "audit", Cutoff: cutoff, Eligible: eligible, Deleted: deleted, CreatedAt: s.now(),
	}).Error
	s.audit(ctx, nil, System(0), "audit.prune", "", map[string]any{"cutoff": cutoff, "eligible": eligible, "deleted": deleted})
}

func (s *Service) countOldAudit(ctx context.Context, cutoff time.Time) int64 {
	var n int64
	_ = s.db.WithContext(ctx).Model(&store.AuditEntry{}).Where("created_at < ?", cutoff).Count(&n).Error
	return n
}
