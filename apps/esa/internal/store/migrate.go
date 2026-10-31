package store

import (
	"context"
	"fmt"
	"log/slog"

	"gorm.io/gorm"
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

// postMigrate holds schema steps AutoMigrate cannot express. Each step must
// be safe to run on every start.
func (s *Store) postMigrate(ctx context.Context) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// One-time steps record their name here when they have run.
		if err := tx.Exec(`CREATE TABLE IF NOT EXISTS schema_flags (name text PRIMARY KEY, done_at timestamptz NOT NULL)`).Error; err != nil {
			return err
		}
		res := tx.Exec(`INSERT INTO schema_flags (name, done_at) VALUES ('personal_spaces_backfill', now()) ON CONFLICT (name) DO NOTHING`)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // the backfill ran on an earlier start
		}
		// Runs once. Accounts created before personal spaces existed: a
		// user's oldest account where they are the only member and its owner
		// becomes their personal space. Users that already have one are
		// skipped, and so are accounts created after the first personal
		// space (teams made once the feature shipped) and accounts that
		// received shared groups (personal spaces never do).
		return tx.Exec(`UPDATE accounts a SET personal_of = pick.user_id
		 FROM (
		   SELECT DISTINCT ON (m.user_id) m.user_id, m.account_id
		   FROM memberships m
		   JOIN accounts acc ON acc.id = m.account_id
		   WHERE m.role = 'owner'
		     AND acc.personal_of IS NULL
		     AND acc.created_at < COALESCE((SELECT min(p.created_at) FROM accounts p WHERE p.personal_of IS NOT NULL), 'infinity')
		     AND NOT EXISTS (SELECT 1 FROM accounts p WHERE p.personal_of = m.user_id)
		     AND NOT EXISTS (SELECT 1 FROM group_shares gs WHERE gs.account_id = acc.id)
		     AND (SELECT count(*) FROM memberships o WHERE o.account_id = m.account_id) = 1
		   ORDER BY m.user_id, acc.created_at, acc.id
		 ) pick
		 WHERE a.id = pick.account_id`).Error
	})
}
