package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestOutboundAgentIsolationRotationAndClaimOnce(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "agentowner")
	now := s.now()
	owner.SignedInAt = &now
	other := signIn(t, s, "agentother")
	other.SignedInAt = &now
	tokenActor := owner
	tokenActor.TokenID = 99
	if _, _, err := s.CreateAgentServer(ctx, tokenActor, "bad", nil); err == nil {
		t.Fatal("token enrolled agent")
	}
	stale := owner
	signedIn := now.Add(-16 * time.Minute)
	stale.SignedInAt = &signedIn
	var reauth *ConflictError
	if _, _, err := s.CreateAgentServer(ctx, stale, "stale", nil); !errors.As(err, &reauth) || reauth.Code != "reauth_required" {
		t.Fatalf("stale enrollment must request a fresh sign-in: %v", err)
	}
	viewer := owner
	viewer.Role = RoleViewer
	if _, _, err := s.CreateAgentServer(ctx, viewer, "bad", nil); !errors.Is(err, ErrForbidden) {
		t.Fatal("viewer enrolled agent", err)
	}
	id, secret, err := s.CreateAgentServer(ctx, owner, "outbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	id2, secret2, err := s.CreateAgentServer(ctx, other, "other", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = id2
	if _, err = s.RotateAgentCredential(ctx, other, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross account rotation", err)
	}
	otherStore, openErr := store.Open(ctx, os.Getenv("ESA_TEST_DATABASE_URL"), s.log)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer otherStore.Close()
	otherProcess := &Service{db: otherStore.DB(), box: s.box, now: s.now}
	a, err := s.AuthenticateAgent(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.AuthenticateAgent(ctx, secret2)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.AuthenticateToken(ctx, secret, SourceAPI); ok {
		t.Fatal("machine token became user")
	}
	var srv store.Server
	s.db.First(&srv, id)
	done := make(chan error, 1)
	go func() {
		var v proto.VersionInfo
		res, e := s.agentCall(ctx, srv, "version")
		if e == nil {
			e = json.Unmarshal(res.Stdout, &v)
		}
		done <- e
	}()
	var cmd *AgentCommand
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		cmd, err = otherProcess.PollAgent(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		if cmd != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cmd == nil {
		t.Fatal("no queued command")
	}
	if again, e := s.PollAgent(ctx, a); e != nil || again != nil {
		t.Fatal("claimed command redelivered", e)
	}
	if hidden, e := s.PollAgent(ctx, b); e != nil || hidden != nil {
		t.Fatal("cross server claim", e)
	}
	payload := `{"ok":true,"protocol":1,"data":{"version":"test"}}`
	if e := s.SubmitAgentResult(ctx, b, cmd.ID, payload, 0); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross server result", e)
	}
	if err = s.SubmitAgentResult(ctx, a, cmd.ID, payload, 0); err != nil {
		t.Fatal(err)
	}
	if err = s.SubmitAgentResult(ctx, a, cmd.ID, `{"ok":false,"error":"overwrite"}`, 1); err != nil {
		t.Fatal("retry refused", err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	var task store.AgentTask
	s.db.First(&task, cmd.ID)
	if task.CommandEnc != "" || task.ResultEnc != "" {
		t.Fatal("consumed evidence retained")
	}
	if err = s.SubmitAgentResult(ctx, a, cmd.ID, payload, 0); err != nil {
		t.Fatal("receipt retry failed", err)
	}
	next, err := s.RotateAgentCredential(ctx, owner, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AuthenticateAgent(ctx, secret); !errors.Is(err, ErrForbidden) {
		t.Fatal("old token still works")
	}
	if _, err = s.PollAgent(ctx, a); !errors.Is(err, ErrForbidden) {
		t.Fatal("old actor still claims")
	}
	fresh, err := s.AuthenticateAgent(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	if e := s.SubmitAgentResult(ctx, fresh, cmd.ID, payload, 0); !errors.Is(e, ErrNotFound) {
		t.Fatal("new generation wrote old command", e)
	}
	if err = s.db.Model(&store.AgentCredential{}).Where("server_id = ?", id).Update("expires_at", s.now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = s.AuthenticateAgent(ctx, next); !errors.Is(err, ErrForbidden) {
		t.Fatal("expired credential authenticated", err)
	}
	if _, err = otherProcess.PollAgent(ctx, fresh); !errors.Is(err, ErrForbidden) {
		t.Fatal("expired actor claimed work", err)
	}
	if err = s.RevokeAgentCredential(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.agentCall(ctx, srv, "version"); err == nil {
		t.Fatal("revoked agent fell back to SSH")
	}
	var audit []store.AuditEntry
	s.db.Find(&audit)
	raw, _ := json.Marshal(audit)
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), next) {
		t.Fatal("audit contains token")
	}
}
func TestOutboundAgentCancellationAndAtRestEncryption(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "agentcancel")
	now := s.now()
	owner.SignedInAt = &now
	id, secret, err := s.CreateAgentServer(ctx, owner, "outbound", nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.AuthenticateAgent(ctx, secret)
	var srv store.Server
	s.db.First(&srv, id)
	cctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { _, err := s.agentCall(cctx, srv, "logs", "--unit", "secret-text.service"); done <- err }()
	var task store.AgentTask
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		s.db.Where("server_id = ?", id).First(&task)
		if task.ID > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if task.ID == 0 || task.CommandEnc == "" || strings.Contains(task.CommandEnc, "secret-text") {
		t.Fatal("command not encrypted")
	}
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if cmd, e := s.PollAgent(ctx, a); e != nil || cmd != nil {
		t.Fatal("cancelled command claimed", e)
	}
	if _, err = s.agentCall(ctx, srv, "upgrade-exec"); err == nil {
		t.Fatal("internal action allowed")
	}
	if _, err = s.agentCall(ctx, srv, "upgrade-start"); err == nil {
		t.Fatal("non-idempotent upgrade allowed")
	}
}
