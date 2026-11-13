package fleet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestPlanApprovalIsABrowserSession(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "plan-owner")
	srv := store.Server{AccountID: owner.AccountID, Name: "web", Host: "10.0.0.8", Port: 22, Username: "esa", Enrollment: "pending", ProtectedContainers: store.JSON("[]"),
		HelperCapabilities: store.MustJSON(proto.Capabilities()), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	state := store.ServerState{ServerID: srv.ID, Status: "ok", Reasons: store.JSON("[]"), Updates: store.MustJSON(map[string]any{"packages": []map[string]any{{"name": "openssl", "security": true}}})}
	if err := s.db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	token := owner
	token.Source, token.TokenID, token.Scopes = SourceMCP, 9, []Scope{ScopeRead, ScopeActions}
	plan, err := s.ProposePlan(ctx, token, PlanBody{Kind: RunUpdate, ServerIDs: []int64{srv.ID}, UpdateMode: "security"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApprovePlan(ctx, token, plan.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a token must not approve, got %v", err)
	}
	approved, err := s.ApprovePlan(ctx, owner, plan.ID)
	if err != nil || approved.Status != "approved" || approved.ExpiresAt == nil {
		t.Fatalf("approve: %+v %v", approved, err)
	}
	logs := owner
	logs.Source, logs.TokenID, logs.Scopes = SourceAPI, 3, []Scope{ScopeActions, ScopeLogs}
	if _, err := s.ExecutePlan(ctx, logs, plan.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a logs token must not execute, got %v", err)
	}
	exec := owner
	exec.Source, exec.TokenID, exec.Scopes = SourceAPI, 4, []Scope{ScopeActions}
	run, err := s.ExecutePlan(ctx, exec, plan.ID)
	if err != nil || run.ID == 0 {
		t.Fatalf("execute: %+v %v", run, err)
	}
	if run.Options.Approved == nil || len(*run.Options.Approved) != 1 || (*run.Options.Approved)[0] != "openssl" {
		t.Fatalf("a plan run must carry its approved list: %+v", run.Options.Approved)
	}
	again, err := s.ExecutePlan(ctx, exec, plan.ID)
	if err != nil || again.ID != run.ID {
		t.Fatalf("second execute got run %d, want %d (%v)", again.ID, run.ID, err)
	}
	past := s.now().Add(-time.Minute)
	if err := s.db.Model(&store.FixPlan{}).Where("id = ?", plan.ID).Updates(map[string]any{"status": "approved", "run_id": nil, "expires_at": past}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecutePlan(ctx, exec, plan.ID); err == nil {
		t.Fatal("an expired approval must not start work")
	}
	s.Wait()
}

func TestDirectRunStopsWhenAPlanIsRequired(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "plan-gate")
	g, err := s.CreateGroup(ctx, owner, GroupInput{Name: "gated", Timezone: "UTC",
		Clean:   CleanPolicy{Days: []string{"sun"}, Time: "03:00", JournalSize: "200M"},
		Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00", PlanRequired: true}})
	if err != nil {
		t.Fatal(err)
	}
	srv := store.Server{AccountID: owner.AccountID, GroupID: &g.ID, Name: "web", Host: "10.0.0.9", Port: 22, Username: "esa", Enrollment: "pending", ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	upd := store.MustJSON(map[string]any{"packages": []map[string]any{{"name": "openssl", "security": true}}})
	if err := s.db.Create(&store.ServerState{ServerID: srv.ID, Status: "ok", Reasons: store.JSON("[]"), Updates: upd}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRun(ctx, owner, RunRequest{Kind: RunUpdate, GroupID: &g.ID}); err == nil {
		t.Fatal("a direct update must wait for a plan")
	}
	plan, err := s.ProposePlan(ctx, owner, PlanBody{Kind: RunUpdate, GroupID: &g.ID, UpdateMode: "security"})
	if err != nil || len(plan.Body.Packages) != 1 || plan.Body.Packages[0] != "openssl" {
		t.Fatalf("propose: %+v %v", plan.Body.Packages, err)
	}
	if err := s.db.Model(&store.ServerState{}).Where("server_id = ?", srv.ID).Update("updates", store.MustJSON(map[string]any{"packages": []map[string]any{{"name": "bash", "security": true}}})).Error; err != nil {
		t.Fatal(err)
	}
	approved, err := s.ApprovePlan(ctx, owner, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	exec := owner
	exec.Source, exec.TokenID, exec.Scopes = SourceAPI, 4, []Scope{ScopeActions}
	if _, err := s.ExecutePlan(ctx, exec, approved.ID); err == nil {
		t.Fatal("a changed package list must invalidate the approval")
	}
	s.Wait()
}

func TestEmptyUpdatePlanIsRefused(t *testing.T) {
	s := newTestService(t)
	owner := signIn(t, s, "plan-empty")
	srv := store.Server{AccountID: owner.AccountID, Name: "web", Host: "10.0.0.7", Port: 22, Username: "esa", Enrollment: "pending", ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	upd := store.MustJSON(map[string]any{"packages": []map[string]any{{"name": "vim", "security": false}}})
	if err := s.db.Create(&store.ServerState{ServerID: srv.ID, Status: "ok", Reasons: store.JSON("[]"), Updates: upd}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProposePlan(context.Background(), owner, PlanBody{Kind: RunUpdate, ServerIDs: []int64{srv.ID}, UpdateMode: "security"}); err == nil {
		t.Fatal("a security plan with no security updates was stored")
	}
	if _, err := s.ProposePlan(context.Background(), owner, PlanBody{Kind: RunUpdate, ServerIDs: []int64{srv.ID}, UpdateMode: "all"}); err != nil {
		t.Fatalf("a full plan with one pending package: %v", err)
	}
}
