package store

import (
	"context"
	"fmt"
	"log/slog"
)

// migrationLockID is the advisory lock key that serialises schema changes
// when more than one control plane process starts at the same time.
const migrationLockID int64 = 0x657361_6d6967 // "esa" "mig"

// Migrate brings the schema up to date with the models in models.go using
// GORM AutoMigrate. AutoMigrate adds tables, columns, indexes, and
// constraints. It never drops or renames anything. A drop or a rename needs
// a hand-written step in postMigrate.
func (s *Store) Migrate(ctx context.Context, log *slog.Logger) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	// A session-level advisory lock held on this connection. Other control
	// plane processes block here until this one has finished migrating.
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("take migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockID)
	}()

	if err := s.db.WithContext(ctx).AutoMigrate(Models()...); err != nil {
		return fmt.Errorf("auto-migrate: %w", err)
	}
	if err := s.postMigrate(ctx); err != nil {
		return fmt.Errorf("post-migrate: %w", err)
	}
	log.Info("database schema up to date", "tables", len(Models()))
	return nil
}

// postMigrate holds schema steps AutoMigrate cannot express. Each step must
// be safe to run on every start.
func (s *Store) postMigrate(ctx context.Context) error {
	// Left over from a one-time account conversion. Nothing reads this table.
	if err := s.db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS schema_flags`).Error; err != nil {
		return err
	}
	// One row per existing user, so a provider sign-in still finds them
	// after identities became their own table.
	return s.db.WithContext(ctx).Exec(`
		INSERT INTO user_identities (user_id, issuer, subject, created_at)
		SELECT u.id, u.issuer, u.subject, u.created_at
		FROM users u
		WHERE NOT EXISTS (
			SELECT 1 FROM user_identities i
			WHERE i.issuer = u.issuer AND i.subject = u.subject
		)
	`).Error
}
