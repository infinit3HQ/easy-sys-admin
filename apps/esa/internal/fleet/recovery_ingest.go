package fleet

import (
	"context"
	"errors"
	"regexp"

	"gorm.io/gorm"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const recoveryPostMax = 64 << 10

var idempotencyKeyRe = regexp.MustCompile(`^[A-Za-z0-9._-]{8,80}$`)

// SubmitRecovery stores one backup result for a server this account owns.
// The same idempotency key returns the first row and does not change it.
// It does not run a backup or a restore.
func (s *Service) SubmitRecovery(ctx context.Context, a Actor, serverID int64, key string, raw []byte) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	if len(raw) == 0 || len(raw) > recoveryPostMax {
		return invalid("report", "must be a JSON object of at most 64 KiB")
	}
	if !idempotencyKeyRe.MatchString(key) {
		return invalid("idempotencyKey", "use 8 to 80 letters, digits, dots, underscores, or hyphens")
	}
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return err
	}
	src, err := hostops.ParseRecovery(raw)
	if err != nil {
		return invalid("report", "%s", err.Error())
	}
	var existing store.IngestedRecovery
	err = s.db.WithContext(ctx).Where("account_id = ? AND idempotency_key = ?", srv.AccountID, key).Take(&existing).Error
	if err == nil {
		if existing.ServerID != srv.ID {
			return &ConflictError{Message: "that idempotency key was already used for another server"}
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	row := store.IngestedRecovery{
		AccountID: srv.AccountID, ServerID: srv.ID, IdempotencyKey: key,
		Dataset: src.Dataset, Tool: src.Tool, Snapshot: src.Snapshot,
		BackupState: src.BackupState, BackupAt: src.BackupAt,
		CheckKind: src.CheckKind, CheckState: src.CheckState, CheckAt: src.CheckAt,
		RestoreState: src.RestoreState, SmokeState: src.SmokeState, ReceivedAt: s.now(),
	}
	if err := s.db.WithContext(ctx).Omit("Account", "Server").Create(&row).Error; err != nil {
		return err
	}
	s.audit(ctx, nil, a, "recovery.submit", srv.Name, map[string]any{"dataset": src.Dataset, "backup": src.BackupState})
	return nil
}

func (s *Service) attachIngested(ctx context.Context, serverID int64, rep *proto.RecoveryReport) {
	var rows []store.IngestedRecovery
	if err := s.db.WithContext(ctx).Where("server_id = ?", serverID).Order("received_at desc, id desc").Limit(20).Find(&rows).Error; err != nil {
		return
	}
	for _, r := range rows {
		rep.Sources = append(rep.Sources, proto.RecoverySource{
			Tool: r.Tool, Dataset: r.Dataset, Snapshot: r.Snapshot,
			BackupState: r.BackupState, BackupAt: r.BackupAt,
			CheckKind: r.CheckKind, CheckState: r.CheckState, CheckAt: r.CheckAt,
			RestoreState: r.RestoreState, SmokeState: r.SmokeState,
		})
	}
}
