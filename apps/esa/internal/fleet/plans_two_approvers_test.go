package fleet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestPlanNeedsTwoDistinctCurrentAdmins(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	now := s.now().UTC().Truncate(time.Second).Add(123456789 * time.Nanosecond)
	s.now = func() time.Time { return now }
	alice, bob := signIn(t, s, "two-alice"), signIn(t, s, "two-bob")
	org, err := s.CreateOrganization(ctx, alice, "Two approvals")
	if err != nil {
		t.Fatal(err)
	}
	owner := inTeam(alice, org)
	admin := inTeam(bob, joinTeam(t, s, owner, bob, RoleAdmin))
	input := GroupInput{Name: "prod", Timezone: "UTC", Clean: CleanPolicy{Days: []string{"sun"}, Time: "03:00", JournalSize: "200M"}, Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00", PlanRequired: true, TwoApprovers: true}}
	g, err := s.CreateGroup(ctx, owner, input)
	if err != nil {
		t.Fatal(err)
	}
	srv := addTestServer(t, s, owner, &g.ID, "web")
	weakerInput := input
	weakerInput.Name = "one-approval"
	weakerInput.Updates.TwoApprovers = false
	weakerGroup, err := s.CreateGroup(ctx, owner, weakerInput)
	if err != nil {
		t.Fatal(err)
	}
	machine := owner
	machine.TokenID = 99
	machine.Source = SourceAPI
	machine.Scopes = []Scope{ScopeAdmin, ScopeActions}
	if _, err = s.UpdateServer(ctx, machine, srv.ID, UpdateServerInput{GroupID: &weakerGroup.ID}); err == nil {
		t.Fatal("token lowered approvals by moving the server")
	}
	if err = s.db.Model(&store.Server{}).Where("id = ?", srv.ID).Update("group_id", weakerGroup.ID).Error; err == nil {
		t.Fatal("legacy writer lowered approvals by moving the server")
	}
	if err = s.db.Delete(&store.Group{}, g.ID).Error; err == nil {
		t.Fatal("legacy writer deleted two-admin group")
	}
	if _, err = s.UpdateServer(ctx, owner, srv.ID, UpdateServerInput{GroupID: &weakerGroup.ID}); err != nil {
		t.Fatal("person could not move server", err)
	}
	if _, err = s.UpdateServer(ctx, owner, srv.ID, UpdateServerInput{GroupID: &g.ID}); err != nil {
		t.Fatal(err)
	}

	s.db.Model(&srv).Update("helper_capabilities", store.MustJSON(proto.Capabilities()))
	plan, err := s.ProposePlan(ctx, owner, PlanBody{Kind: RunMeasure, GroupID: &g.ID})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ApprovalsRequired != 2 {
		t.Fatal("group approval requirement was not frozen")
	}
	// Simulate an older binary which inserts no approval-policy columns.
	var legacy store.FixPlan
	if err := s.db.Raw(`INSERT INTO fix_plans(account_id,status,fingerprint,body,created_by,proposer_token_id,created_at,updated_at) SELECT account_id,'submitted',fingerprint,body,created_by,proposer_token_id,created_at,updated_at FROM fix_plans WHERE id = ? RETURNING *`, plan.ID).Scan(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if legacy.ApprovalsRequired != 2 {
		t.Fatal("legacy proposal bypassed group policy")
	}
	if err := s.db.Model(&legacy).Updates(map[string]any{"status": "approved", "approved_by": owner.UserID, "approved_at": s.now(), "expires_at": s.now().Add(time.Minute)}).Error; err == nil {
		t.Fatal("legacy approval bypassed two-person quorum")
	}
	if err := s.db.Model(&store.Group{}).Where("id = ?", g.ID).Update("plan_required", false).Error; err == nil {
		t.Fatal("legacy group update weakened the two-person rule")
	}
	first, err := s.ApprovePlan(ctx, owner, plan.ID)
	if err != nil || first.Status != "submitted" || first.Approvals != 1 {
		t.Fatalf("first: %+v %v", first, err)
	}
	again, err := s.ApprovePlan(ctx, owner, plan.ID)
	if err != nil || again.Approvals != 1 {
		t.Fatalf("duplicate: %+v %v", again, err)
	}
	if _, err := s.ExecutePlan(ctx, owner, plan.ID); err == nil {
		t.Fatal("ran with one approval")
	}
	token := admin
	token.TokenID = 99
	token.Source = SourceAPI
	token.Scopes = []Scope{ScopeAdmin, ScopeActions}
	if _, err := s.ApprovePlan(ctx, token, plan.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("token approved: %v", err)
	}
	approved, err := s.ApprovePlan(ctx, admin, plan.ID)
	if err != nil || approved.Status != "approved" || approved.Approvals != 2 {
		t.Fatalf("second: %+v %v", approved, err)
	}
	if !approved.ExpiresAt.Equal(*first.ExpiresAt) {
		t.Fatal("second approval extended expiry")
	}
	weaker := input
	weaker.Updates.TwoApprovers = false
	if _, err := s.UpdateGroup(ctx, token, g.ID, weaker); err == nil {
		t.Fatal("token weakened approvals")
	}
	if _, err := s.UpdateMemberRole(ctx, owner, admin.UserID, RoleViewer); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExecutePlan(ctx, owner, plan.ID); err == nil {
		t.Fatal("ran after an approver lost admin")
	}
	s.Wait()
	if err := s.DeleteOrganization(ctx, owner, "Two approvals"); err != nil {
		t.Fatal("approval guard blocked authorized organization deletion", err)
	}
}
