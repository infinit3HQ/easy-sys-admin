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
// constraints; it never drops or renames anything. Renames and drops need a
// hand-written step in postMigrate.
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

// postMigrate holds schema steps AutoMigrate cannot express. Each statement
// must be safe to run on every start.
func (s *Store) postMigrate(ctx context.Context) error {
	stmts := []string{}
	for _, stmt := range stmts {
		if err := s.db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}
