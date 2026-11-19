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
	if !s.claimJob(ctx, "audit", 30*time.Minute) {
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

// PruneReports removes expired resolved-notice history and superseded scan
// snapshots. The latest scan survives even when stale, so absence never looks
// like a clean scan. Recovery idempotency records and run reports are retained.
func (s *Service) PruneReports(ctx context.Context) {
	if s.reportRetention <= 0 || (!s.auditHoldUntil.IsZero() && s.now().Before(s.auditHoldUntil)) {
		return
	}
	if !s.claimJob(ctx, "reports", 30*time.Minute) {
		return
	}
	cutoff := s.now().Add(-s.reportRetention)
	var deleted, eligible int64
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		db := q.Raw().WithContext(ctx)
		if err := db.Raw(`SELECT (SELECT count(*) FROM security_incidents WHERE state = 'resolved' AND resolved_at < ?) + (SELECT count(*) FROM scan_imports candidate WHERE candidate.imported_at < ? AND EXISTS (SELECT 1 FROM scan_imports newer WHERE newer.account_id = candidate.account_id AND newer.server_id = candidate.server_id AND (newer.imported_at,newer.id) > (candidate.imported_at,candidate.id)))`, cutoff, cutoff).Scan(&eligible).Error; err != nil {
			return err
		}
		for _, query := range []string{
			`DELETE FROM security_incidents WHERE id IN (SELECT id FROM security_incidents WHERE state = 'resolved' AND resolved_at < ? ORDER BY resolved_at LIMIT 1000 FOR UPDATE SKIP LOCKED)`,
			`DELETE FROM scan_imports old WHERE old.id IN (SELECT candidate.id FROM scan_imports candidate WHERE candidate.imported_at < ? AND EXISTS (SELECT 1 FROM scan_imports newer WHERE newer.account_id = candidate.account_id AND newer.server_id = candidate.server_id AND (newer.imported_at,newer.id) > (candidate.imported_at,candidate.id)) ORDER BY candidate.imported_at LIMIT 1000 FOR UPDATE SKIP LOCKED)`,
		} {
			result := db.Exec(query, cutoff)
			if result.Error != nil {
				return result.Error
			}
			deleted += result.RowsAffected
		}
		if err := db.Create(&store.RetentionRun{Kind: "reports", Cutoff: cutoff, Eligible: eligible, Deleted: deleted, CreatedAt: s.now()}).Error; err != nil {
			return err
		}
		s.audit(ctx, q, System(0), "reports.prune", "", map[string]any{"cutoff": cutoff, "deleted": deleted})
		return nil
	})
	if err != nil {
		s.log.Error("prune report history", "err", err)
	}
}
