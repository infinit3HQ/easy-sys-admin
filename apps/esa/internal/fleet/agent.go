package fleet

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// AgentActor is a machine identity, never a user or API-token actor.
// Its fields are private and only AuthenticateAgent can issue one.
type AgentActor struct{ serverID, generation int64 }
type AgentCommand struct {
	ID        int64     `json:"id"`
	Action    string    `json:"action"`
	Args      []string  `json:"args"`
	ExpiresAt time.Time `json:"expiresAt"`
	// ServerID and Signature let an agent that holds a pin check that the
	// control plane, not something in front of it, sent the command.
	ServerID  int64  `json:"serverId,omitempty"`
	Signature string `json:"signature,omitempty"`
}

func agentContext(id int64, kind string) string { return fmt.Sprintf("agent_task:%d:%s", id, kind) }

func (s *Service) agentAdmin(ctx context.Context, a Actor) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	if !a.isPerson() || a.SignedInAt == nil || s.now().Sub(*a.SignedInAt) > 15*time.Minute || a.SignedInAt.After(s.now().Add(time.Minute)) {
		return &ConflictError{Code: "reauth_required", Message: "Sign in again before managing an outbound connection."}
	}
	return s.member(ctx, a)
}

// CreateAgentServer registers an outbound-only server without an SSH key.
func (s *Service) CreateAgentServer(ctx context.Context, a Actor, name string, groupID *int64) (int64, string, error) {
	if err := s.agentAdmin(ctx, a); err != nil {
		return 0, "", err
	}
	// The pin is read after the credential is made. Make sure the key can be
	// read now, so a failure does not leave a credential nobody is shown.
	if _, err := s.accountKey(ctx, a.AccountID); err != nil {
		return 0, "", err
	}
	srv, _, err := s.validateNewServer(ctx, a, name, "outbound.invalid", 22, groupID, "")
	if err != nil {
		return 0, "", err
	}
	srv.Connection = "outbound"
	srv.Enrollment = "ready"
	secret, hash := randomToken("esa_agent_")
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if _, ms, err := s.lockMembers(ctx, q, a); err != nil {
			return err
		} else if m, ok := findMember(ms, a.UserID); !ok || !Role(m.Role).AtLeast(RoleAdmin) {
			return ErrForbidden
		}
		if err := s.capLocked(q.Raw(), a.AccountID, &store.Server{}, s.maxServers, "servers"); err != nil {
			return err
		}
		if err := q.Raw().Omit(clause.Associations).Create(&srv).Error; err != nil {
			return mapStore(err)
		}
		if err := q.Raw().Create(&store.ServerState{ServerID: srv.ID, Status: string(StatusPending), UpdatedAt: s.now()}).Error; err != nil {
			return err
		}
		if err := q.Raw().Omit(clause.Associations).Create(&store.AgentCredential{ServerID: srv.ID, AccountID: a.AccountID, TokenHash: hash, Generation: 1, ExpiresAt: s.now().Add(365 * 24 * time.Hour), CreatedAt: s.now()}).Error; err != nil {
			return err
		}
		s.audit(ctx, q, a, "agent.create", srv.Name, nil)
		return nil
	})
	return srv.ID, secret, err
}

// RotateAgentCredential invalidates credentials and pending work atomically.
func (s *Service) RotateAgentCredential(ctx context.Context, a Actor, id int64) (string, error) {
	if err := s.agentAdmin(ctx, a); err != nil {
		return "", err
	}
	if _, err := s.accountKey(ctx, a.AccountID); err != nil {
		return "", err
	}
	srv, err := s.loadServer(ctx, a, id)
	if err != nil {
		return "", err
	}
	if srv.Connection != "outbound" {
		return "", invalid("serverId", "this server uses SSH")
	}
	secret, hash := randomToken("esa_agent_")
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if _, ms, err := s.lockMembers(ctx, q, a); err != nil {
			return err
		} else if m, ok := findMember(ms, a.UserID); !ok || !Role(m.Role).AtLeast(RoleAdmin) {
			return ErrForbidden
		}
		r := q.Raw().Model(&store.AgentCredential{}).Where("server_id = ? AND account_id = ?", id, a.AccountID).Updates(map[string]any{"token_hash": hash, "expires_at": s.now().Add(365 * 24 * time.Hour), "generation": gorm.Expr("generation + 1"), "revoked": false})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return ErrNotFound
		}
		if err := q.Raw().Model(&store.AgentTask{}).Where("server_id = ?", id).Updates(map[string]any{"state": "cancelled", "command_enc": "", "result_enc": ""}).Error; err != nil {
			return err
		}
		s.audit(ctx, q, a, "agent.rotate", srv.Name, nil)
		return nil
	})
	return secret, err
}
func (s *Service) RevokeAgentCredential(ctx context.Context, a Actor, id int64) error {
	if err := s.agentAdmin(ctx, a); err != nil {
		return err
	}
	srv, err := s.loadServer(ctx, a, id)
	if err != nil {
		return err
	}
	return s.store.InTx(ctx, func(q *store.Queries) error {
		if _, ms, err := s.lockMembers(ctx, q, a); err != nil {
			return err
		} else if m, ok := findMember(ms, a.UserID); !ok || !Role(m.Role).AtLeast(RoleAdmin) {
			return ErrForbidden
		}
		r := q.Raw().Model(&store.AgentCredential{}).Where("server_id = ? AND account_id = ?", id, a.AccountID).Updates(map[string]any{"revoked": true, "generation": gorm.Expr("generation + 1")})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return ErrNotFound
		}
		if err := q.Raw().Model(&store.AgentTask{}).Where("server_id = ?", id).Updates(map[string]any{"state": "cancelled", "command_enc": "", "result_enc": ""}).Error; err != nil {
			return err
		}
		s.audit(ctx, q, a, "agent.revoke", srv.Name, nil)
		return nil
	})
}
func (s *Service) AuthenticateAgent(ctx context.Context, secret string) (AgentActor, error) {
	if !strings.HasPrefix(secret, "esa_agent_") || len(secret) > 200 {
		return AgentActor{}, ErrForbidden
	}
	var row store.AgentCredential
	if err := s.db.WithContext(ctx).Where("token_hash = ? AND revoked = false AND expires_at > ?", hashToken(secret), s.now()).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return AgentActor{}, ErrForbidden
		}
		return AgentActor{}, err
	}
	return AgentActor{row.ServerID, row.Generation}, nil
}

// withAgent locks the current credential until the claim/result commits, so
// rotation cannot race an old generation into a new queue.
func (s *Service) withAgent(ctx context.Context, a AgentActor, fn func(*gorm.DB) error) error {
	if a.serverID == 0 {
		return ErrForbidden
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row store.AgentCredential
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("server_id = ? AND generation = ? AND revoked = false AND expires_at > ?", a.serverID, a.generation, s.now()).Take(&row).Error; err != nil {
			return ErrForbidden
		}
		if err := tx.Model(&row).Update("last_seen_at", s.now()).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}
func (s *Service) PollAgent(ctx context.Context, a AgentActor) (*AgentCommand, error) {
	var out *AgentCommand
	err := s.withAgent(ctx, a, func(tx *gorm.DB) error {
		var row store.AgentTask
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("server_id = ? AND generation = ? AND state = 'queued' AND expires_at > ?", a.serverID, a.generation, s.now()).Order("id").Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		raw, err := s.box.Open(row.CommandEnc, agentContext(row.ID, "command"))
		if err != nil {
			return err
		}
		var cmd AgentCommand
		if err = json.Unmarshal(raw, &cmd); err != nil {
			return err
		}
		if err = tx.Model(&row).Updates(map[string]any{"state": "claimed", "command_enc": ""}).Error; err != nil {
			return err
		}
		out = &cmd
		return nil
	})
	return out, err
}
func (s *Service) SubmitAgentResult(ctx context.Context, a AgentActor, id int64, output string, exit int) error {
	if len(output) > 4<<20 {
		return invalid("output", "helper output is too large")
	}
	var envelope proto.Envelope
	if json.Unmarshal([]byte(output), &envelope) != nil {
		return invalid("output", "expected a helper envelope")
	}
	raw, _ := json.Marshal(remote.ExecResult{Stdout: []byte(output), Exit: exit})
	return s.withAgent(ctx, a, func(tx *gorm.DB) error {
		var row store.AgentTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND server_id = ? AND generation = ?", id, a.serverID, a.generation).Take(&row).Error; err != nil {
			return ErrNotFound
		}
		if !s.now().Before(row.ExpiresAt) {
			return &ConflictError{Message: "command expired"}
		}
		if row.State == "done" {
			return nil
		}
		if row.State != "claimed" {
			return &ConflictError{Message: "command is not claimed"}
		}
		sealed, err := s.box.Seal(raw, agentContext(row.ID, "result"))
		if err != nil {
			return err
		}
		return tx.Model(&row).Updates(map[string]any{"state": "done", "result_enc": sealed}).Error
	})
}
func (s *Service) agentCall(ctx context.Context, srv store.Server, action string, args ...string) (remote.ExecResult, error) {
	fail := func(msg string) (remote.ExecResult, error) {
		return remote.ExecResult{}, &UnavailableError{Message: msg}
	}
	if !slices.Contains(proto.Actions, action) || action == "upgrade-exec" || action == "uninstall" {
		return fail("this helper action is unavailable through an outbound connection")
	}
	if action == "upgrade-start" && !proto.Supports(storedCapabilities(srv), proto.FeatureUpgradeIdempotent) {
		return fail("outbound upgrades require a helper with upgrade-idempotent support")
	}
	if len(args) > 256 {
		return fail("too many helper arguments")
	}
	for _, a := range args {
		if len(a) > 65536 {
			return fail("helper argument is too large")
		}
	}
	now := s.now()
	expires := now.Add(10 * time.Minute)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(expires) {
		expires = deadline
	}
	if !expires.After(now) {
		return remote.ExecResult{}, ctx.Err()
	}
	signer, err := s.accountKey(ctx, srv.AccountID)
	if err != nil {
		return fail("could not sign the outbound command")
	}
	var row store.AgentTask
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cred store.AgentCredential
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("server_id = ? AND account_id = ? AND revoked = false AND expires_at > ?", srv.ID, srv.AccountID, s.now()).Take(&cred).Error; err != nil {
			return &UnavailableError{Message: "outbound connection is disabled"}
		}
		var count int64
		if err := tx.Model(&store.AgentTask{}).Where("server_id = ? AND state IN ('queued','claimed') AND expires_at > ?", srv.ID, now).Count(&count).Error; err != nil {
			return err
		}
		if count >= 32 {
			return &UnavailableError{Message: "outbound command queue is full"}
		}
		row = store.AgentTask{ServerID: srv.ID, Generation: cred.Generation, State: "queued", ExpiresAt: expires, CreatedAt: now}
		if err := tx.Omit(clause.Associations).Create(&row).Error; err != nil {
			return err
		}
		if args == nil {
			args = []string{}
		}
		command := AgentCommand{ID: row.ID, Action: action, Args: args, ExpiresAt: expires, ServerID: srv.ID}
		command.Signature, err = signAgentCommand(signer, command)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(command)
		sealed, err := s.box.Seal(raw, agentContext(row.ID, "command"))
		if err != nil {
			return err
		}
		return tx.Model(&row).Update("command_enc", sealed).Error
	})
	if err != nil {
		return remote.ExecResult{}, err
	}
	// Clear sensitive evidence when consumed or abandoned. A short done receipt
	// remains until expiry for idempotent result retries; it carries no output.
	defer func() {
		clean, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		s.db.WithContext(clean).Model(&store.AgentTask{}).Where("id = ?", row.ID).Updates(map[string]any{"state": gorm.Expr("CASE WHEN state = 'done' THEN 'done' ELSE 'cancelled' END"), "command_enc": "", "result_enc": ""})
	}()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	timer := time.NewTimer(time.Until(expires))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return remote.ExecResult{}, ctx.Err()
		case <-timer.C:
			return fail("outbound server did not return a result before the command expired")
		case <-tick.C:
		}
		var task store.AgentTask
		if err := s.db.WithContext(ctx).Where("id = ?", row.ID).Take(&task).Error; err != nil {
			return remote.ExecResult{}, err
		}
		if task.State == "cancelled" {
			return fail("outbound command was cancelled")
		}
		if task.State != "done" {
			continue
		}
		raw, err := s.box.Open(task.ResultEnc, agentContext(row.ID, "result"))
		if err != nil {
			return remote.ExecResult{}, err
		}
		var res remote.ExecResult
		err = json.Unmarshal(raw, &res)
		return res, err
	}
}

// PruneAgentTasks keeps only unexpired receipts. No task output is an audit log.
func (s *Service) PruneAgentTasks(ctx context.Context) {
	s.db.WithContext(ctx).Where("expires_at < ?", s.now()).Delete(&store.AgentTask{})
}

// signAgentCommand signs a command with the account key. The agent checks it
// against the pin from AgentSigningPin.
func signAgentCommand(k *accountSigner, c AgentCommand) (string, error) {
	sig, err := k.signer.Sign(rand.Reader, proto.AgentCommandMessage(c.ServerID, c.ID, c.Action, c.Args, c.ExpiresAt.Unix()))
	if err != nil {
		return "", err
	}
	if sig.Format != ssh.KeyAlgoED25519 || len(sig.Blob) != ed25519.SignatureSize {
		return "", errors.New("the account key does not make Ed25519 signatures")
	}
	return base64.StdEncoding.EncodeToString(sig.Blob), nil
}

// AgentSigningPin returns the pin an outbound agent is set up with: the server
// it serves and the public key that signs its commands. It is shown once with
// the credential and given to the agent out of band, so it does not depend on
// the connection the commands arrive on.
func (s *Service) AgentSigningPin(ctx context.Context, accountID, serverID int64) (string, error) {
	k, err := s.accountKey(ctx, accountID)
	if err != nil {
		return "", err
	}
	cp, ok := k.signer.PublicKey().(ssh.CryptoPublicKey)
	if !ok {
		return "", errors.New("the account key has no public key to pin")
	}
	pub, ok := cp.CryptoPublicKey().(ed25519.PublicKey)
	if !ok {
		return "", errors.New("the account key is not an Ed25519 key")
	}
	return proto.FormatAgentPin(serverID, pub), nil
}
