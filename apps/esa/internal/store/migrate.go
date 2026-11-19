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
	if err := s.installPlanApprovalGuard(ctx); err != nil {
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

// Keep two-person approvals enforced while an older control plane shares
// the database. Older binaries do not know the new approval columns.
func (s *Store) installPlanApprovalGuard(ctx context.Context) error {
	return s.db.WithContext(ctx).Exec(`
 CREATE OR REPLACE FUNCTION esa_plan_approval_guard() RETURNS trigger AS $$
 BEGIN
  IF EXISTS (
   SELECT 1 FROM groups g WHERE g.account_id = NEW.account_id AND g.two_approvers AND (
    g.id::text = NEW.body->>'groupId' OR EXISTS (
     SELECT 1 FROM servers srv WHERE srv.account_id = NEW.account_id AND srv.group_id = g.id
      AND ((NEW.body->'serverIds') @> jsonb_build_array(srv.id) OR (NEW.body->'pinnedServerIds') @> jsonb_build_array(srv.id))
    )
   )
  ) THEN NEW.approvals_required := GREATEST(NEW.approvals_required, 2); END IF;
  IF NEW.status IN ('approved','executing') AND NEW.approvals_required >= 2 THEN
   IF NEW.approved_by IS NULL OR NEW.second_approved_by IS NULL OR NEW.approved_by = NEW.second_approved_by THEN
    RAISE EXCEPTION 'two distinct admin approvals are required' USING ERRCODE = '23514';
   END IF;
   IF (SELECT count(*) FROM memberships WHERE account_id = NEW.account_id AND user_id IN (NEW.approved_by,NEW.second_approved_by) AND role IN ('owner','admin')) <> 2 THEN
    RAISE EXCEPTION 'both approvers must remain admins' USING ERRCODE = '23514';
   END IF;
  END IF;
  RETURN NEW;
 END;
 $$ LANGUAGE plpgsql;
 CREATE OR REPLACE TRIGGER esa_plan_approval_guard
 BEFORE INSERT OR UPDATE ON fix_plans
 FOR EACH ROW EXECUTE FUNCTION esa_plan_approval_guard();

 -- An older binary cannot identify two-admin policies. Refuse its move or
 -- delete rather than allowing it to weaken a policy it does not understand.
 -- Current browser writes set a transaction-local marker after permission checks.
 CREATE OR REPLACE FUNCTION esa_two_approver_membership_guard() RETURNS trigger AS $$
 DECLARE from_two boolean; to_two boolean;
 BEGIN
  IF current_setting('esa.person_write',true) = 'true' THEN
   IF TG_OP = 'DELETE' THEN RETURN OLD; ELSE RETURN NEW; END IF;
  END IF;
  IF TG_OP = 'DELETE' THEN
   IF NOT EXISTS (SELECT 1 FROM accounts WHERE id = OLD.account_id) THEN RETURN OLD; END IF;
   IF OLD.two_approvers THEN RAISE EXCEPTION 'a person must delete a two-admin group' USING ERRCODE = '23514'; END IF;
   RETURN OLD;
  END IF;
  IF OLD.group_id IS NOT DISTINCT FROM NEW.group_id THEN RETURN NEW; END IF;
  SELECT two_approvers INTO from_two FROM groups WHERE id = OLD.group_id FOR SHARE;
  IF COALESCE(from_two,false) THEN
   SELECT two_approvers AND plan_required INTO to_two FROM groups WHERE id = NEW.group_id AND account_id = NEW.account_id FOR SHARE;
   IF NOT COALESCE(to_two,false) THEN RAISE EXCEPTION 'a person must lower the required approvals when moving a server' USING ERRCODE = '23514'; END IF;
  END IF;
  RETURN NEW;
 END;
 $$ LANGUAGE plpgsql;
 CREATE OR REPLACE TRIGGER esa_two_approver_server_move_guard BEFORE UPDATE OF group_id ON servers FOR EACH ROW EXECUTE FUNCTION esa_two_approver_membership_guard();
 CREATE OR REPLACE TRIGGER esa_two_approver_group_delete_guard BEFORE DELETE ON groups FOR EACH ROW EXECUTE FUNCTION esa_two_approver_membership_guard();
 `).Error
}
