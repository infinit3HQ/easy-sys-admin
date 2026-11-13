package fleet

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestHelperGaps(t *testing.T) {
	legacy := store.Server{Name: "old"}
	limited := store.Server{Name: "limited", HelperCapabilities: store.MustJSON([]string{"status", "clean"})}
	current := store.Server{Name: "new", HelperCapabilities: store.MustJSON(proto.Actions)}
	if gaps := helperGaps(runActions(RunUpdate), []store.Server{legacy, current}); len(gaps) != 0 {
		t.Fatalf("legacy and current helpers can update: %v", gaps)
	}
	gaps := helperGaps(runActions(RunUpdate), []store.Server{limited, current})
	if len(gaps) != 1 || gaps[0] != "limited (upgrade-precheck, upgrade-start, upgrade-status)" {
		t.Fatalf("gaps: %v", gaps)
	}
	if got := helperGaps(runActions(RunClean), []store.Server{limited}); len(got) != 0 {
		t.Fatalf("limited can clean: %v", got)
	}
}

func TestServerViewListsMissingHelperActions(t *testing.T) {
	v := toView(store.Server{Name: "old", ProtectedContainers: store.JSON("[]")}, store.ServerState{}, false)
	if !slices.Equal(v.HelperMissing, []string{"recovery", "inventory", proto.FeatureUpgradeAllow}) {
		t.Fatalf("missing: %v", v.HelperMissing)
	}
	now := time.Now()
	v = toView(store.Server{Name: "new", ProtectedContainers: store.JSON("[]"), HelperCapabilities: store.MustJSON(proto.Capabilities()), HelperCheckedAt: &now}, store.ServerState{}, false)
	if len(v.HelperMissing) != 0 || v.HelperCheckedAt == nil {
		t.Fatalf("current helper: %+v", v.HelperMissing)
	}
}

func TestRunRefusedWhenAHelperLacksTheAction(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "helper-gap")
	srv := store.Server{AccountID: owner.AccountID, Name: "limited", Host: "10.0.5.1", Port: 22, Username: "esa", Enrollment: "pending",
		ProtectedContainers: store.JSON("[]"), HelperCapabilities: store.MustJSON([]string{"status"}), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	_, err := s.StartRun(ctx, owner, RunRequest{Kind: RunClean, ServerIDs: []int64{srv.ID}})
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Code != proto.CodeIncompatible {
		t.Fatalf("got %v, want a helper_incompatible conflict", err)
	}
}

func TestPlanUpdateNeedsAHelperThatHonoursTheList(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "allow-gap")
	srv := store.Server{AccountID: owner.AccountID, Name: "old", Host: "10.0.5.2", Port: 22, Username: "esa", Enrollment: "pending",
		ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	approved := []string{"openssl"}
	_, err := s.StartRun(ctx, owner, RunRequest{Kind: RunUpdate, ServerIDs: []int64{srv.ID}, FromPlan: true, Options: &RunOptions{Approved: &approved}})
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Code != proto.CodeIncompatible {
		t.Fatalf("an old helper would ignore the approved list; got %v", err)
	}
	run, err := s.StartRun(ctx, owner, RunRequest{Kind: RunUpdate, ServerIDs: []int64{srv.ID}, Options: &RunOptions{Approved: &approved}})
	if err != nil {
		t.Fatal(err)
	}
	if run.Options.Approved != nil {
		t.Fatal("only a plan may set an approved list")
	}
	s.Wait()
}
