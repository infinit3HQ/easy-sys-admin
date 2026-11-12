package fleet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestPlanApprovalIsABrowserSession(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "plan-owner")
	srv := store.Server{AccountID: owner.AccountID, Name: "web", Host: "10.0.0.8", Port: 22, Username: "esa", Enrollment: "pending", ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	token := owner
	token.Source, token.TokenID, token.Scopes = SourceMCP, 9, []Scope{ScopeLogs}
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
}
