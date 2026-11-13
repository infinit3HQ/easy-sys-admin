package fleet

import (
	"context"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestBeforeChecksOverride(t *testing.T) {
	checks := []HealthCheck{{Name: "api", Kind: "http", Blocking: true}, {Name: "db", Kind: "tcp", Blocking: true}}
	o := &CheckOverride{Checks: []string{"api"}, Reason: "api is down for an unrelated migration"}
	got := beforeChecks(checks, o)
	if got[0].Blocking || !got[1].Blocking || !checks[0].Blocking {
		t.Fatalf("only api is advisory, and the run's list is unchanged: %+v %+v", got, checks)
	}
	res := []CheckResult{{Name: "api", State: "fail"}, {Name: "db", State: "pass"}}
	markOverridden(res, o)
	if !res[0].Overridden || res[1].Overridden {
		t.Fatalf("marks: %+v", res)
	}
}

func TestOverrideRules(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "override")
	srv := store.Server{AccountID: a.AccountID, Name: "web", Host: "10.0.9.1", Port: 22, Username: "esa", Enrollment: "pending",
		ProtectedContainers: store.JSON("[]"), HelperCapabilities: store.MustJSON(proto.Capabilities()), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	checks := []HealthCheck{{Name: "api", Kind: "tcp", Port: 443, Blocking: true}, {Name: "info", Kind: "tcp", Port: 80}}
	req := func(o CheckOverride) RunRequest {
		return RunRequest{Kind: RunUpdate, ServerIDs: []int64{srv.ID}, Options: &RunOptions{Checks: checks}, Override: &o}
	}
	token := a
	token.Source, token.TokenID, token.Scopes = SourceAPI, 2, []Scope{ScopeActions}
	if _, err := s.StartRun(ctx, token, req(CheckOverride{Checks: []string{"api"}, Reason: "known"})); err == nil {
		t.Fatal("a token overrode a check")
	}
	if _, err := s.StartRun(ctx, a, req(CheckOverride{Checks: []string{"api"}})); err == nil {
		t.Fatal("an override without a reason was accepted")
	}
	if _, err := s.StartRun(ctx, a, req(CheckOverride{Checks: []string{"info"}, Reason: "x"})); err == nil {
		t.Fatal("an advisory check was overridden")
	}
	run, err := s.StartRun(ctx, a, req(CheckOverride{Checks: []string{"api"}, Reason: "api down for a migration"}))
	if err != nil {
		t.Fatal(err)
	}
	if run.Options.Override == nil || run.Options.Override.By != a.UserID {
		t.Fatalf("override not frozen on the run: %+v", run.Options.Override)
	}
	s.Wait()
}
