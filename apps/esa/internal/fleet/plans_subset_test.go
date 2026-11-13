package fleet

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func planServer(t *testing.T, s *Service, a Actor, name string, pending ...string) store.Server {
	t.Helper()
	srv := store.Server{AccountID: a.AccountID, Name: name, Host: "10.0.8." + name[len(name)-1:], Port: 22, Username: "esa", Enrollment: "pending",
		ProtectedContainers: store.JSON("[]"), HelperCapabilities: store.MustJSON(proto.Capabilities()), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	pkgs := []map[string]any{}
	for _, p := range pending {
		pkgs = append(pkgs, map[string]any{"name": p, "security": true})
	}
	if err := s.db.Create(&store.ServerState{ServerID: srv.ID, Status: "ok", Reasons: store.JSON("[]"), Updates: store.MustJSON(map[string]any{"packages": pkgs})}).Error; err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestPlanApprovesAChosenSubset(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "plan-subset")
	srv := planServer(t, s, a, "web1", "openssl", "libc6", "libsqlite3-0")
	if _, err := s.ProposePlan(ctx, a, PlanBody{Kind: RunUpdate, ServerIDs: []int64{srv.ID}, UpdateMode: "security", Packages: []string{"nginx"}}); err == nil {
		t.Fatal("a package that is not pending was approved")
	}
	plan, err := s.ProposePlan(ctx, a, PlanBody{Kind: RunUpdate, ServerIDs: []int64{srv.ID}, UpdateMode: "security", Packages: []string{"libsqlite3-0"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(plan.Body.Packages, []string{"libsqlite3-0"}) || len(plan.Body.Pending) != 3 {
		t.Fatalf("approved %v of pending %v", plan.Body.Packages, plan.Body.Pending)
	}
	if _, err := s.ApprovePlan(ctx, a, plan.ID); err != nil {
		t.Fatal(err)
	}
	exec := a
	exec.Source, exec.TokenID, exec.Scopes = SourceAPI, 4, []Scope{ScopeActions}
	run, err := s.ExecutePlan(ctx, exec, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Options.Approved == nil || !slices.Equal(*run.Options.Approved, []string{"libsqlite3-0"}) {
		t.Fatalf("the helper must get only the chosen package: %v", run.Options.Approved)
	}
	s.Wait()
}

func TestCancelAndGateOnPlans(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "plan-cancel")
	one := planServer(t, s, a, "app1", "openssl")
	two := planServer(t, s, a, "app2", "openssl")
	if _, err := s.ProposePlan(ctx, a, PlanBody{Kind: RunUpdate, ServerIDs: []int64{one.ID}, WaitAfterCanaries: true}); err == nil {
		t.Fatal("a gate without a canary was accepted")
	}
	plan, err := s.ProposePlan(ctx, a, PlanBody{Kind: RunUpdate, ServerIDs: []int64{one.ID, two.ID}, CanaryServerIDs: []int64{one.ID}, WaitAfterCanaries: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApprovePlan(ctx, a, plan.ID); err != nil {
		t.Fatal(err)
	}
	cancelled, err := s.CancelPlan(ctx, a, plan.ID, "window moved")
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("cancel: %+v %v", cancelled.Status, err)
	}
	if _, err := s.ExecutePlan(ctx, a, plan.ID); err == nil {
		t.Fatal("a cancelled plan ran")
	}
	if _, err := s.CancelPlan(ctx, a, plan.ID, ""); err == nil {
		t.Fatal("a cancelled plan was cancelled again")
	}
	again, err := s.ProposePlan(ctx, a, PlanBody{Kind: RunUpdate, ServerIDs: []int64{one.ID, two.ID}, CanaryServerIDs: []int64{one.ID}, WaitAfterCanaries: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApprovePlan(ctx, a, again.ID); err != nil {
		t.Fatal(err)
	}
	run, err := s.ExecutePlan(ctx, a, again.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Options.WaitAfterCanaries {
		t.Fatal("the plan's gate did not reach its run")
	}
	s.Wait()
}
