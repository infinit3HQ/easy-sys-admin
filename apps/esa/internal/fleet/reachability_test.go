package fleet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestReachabilityEvidenceFreshnessAndIsolation(t *testing.T) {
	f := newShareFixture(t)
	s := f.s
	ctx := context.Background()
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	input := ProbeInput{Source: "office", Host: f.shared.Host, Port: 443, State: "reachable", Reason: "none", ObservedAt: now}
	if _, err := s.GetReachability(ctx, f.bob, f.shared.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign read: %v", err)
	}
	read := f.alice
	read.Scopes = []Scope{ScopeRead}
	if err := s.SubmitReachability(ctx, read, f.shared.ID, input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read token submitted: %v", err)
	}
	if err := s.SubmitReachability(ctx, f.alice, f.shared.ID, input); err != nil {
		t.Fatal(err)
	}
	f.accept(t, "operate")
	if err := s.SubmitReachability(ctx, f.bob, f.shared.ID, input); !errors.Is(err, ErrNotFound) {
		t.Fatalf("shared write: %v", err)
	}
	got, err := s.GetReachability(ctx, f.bob, f.shared.ID)
	if err != nil || len(got) != 1 || got[0].State != "reachable" || got[0].Stale {
		t.Fatalf("shared read: %+v %v", got, err)
	}
	first := now
	now = now.Add(time.Minute)
	input.ObservedAt = now
	input.State = "unknown"
	input.Reason = "network_error"
	if err := s.SubmitReachability(ctx, f.alice, f.shared.ID, input); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetReachability(ctx, f.alice, f.shared.ID)
	if err != nil || got[0].State != "unknown" || got[0].LastKnownState != "reachable" || !got[0].LastKnownAt.Equal(first) {
		t.Fatalf("unknown erased evidence: %+v %v", got, err)
	}
	input.ObservedAt = first
	input.State = "not_reached"
	input.Reason = "refused"
	if err := s.SubmitReachability(ctx, f.alice, f.shared.ID, input); err != nil {
		t.Fatal(err)
	}
	if n := auditCount(t, s, f.alice.AccountID, "probe.observe", 0); n != 2 {
		t.Fatalf("out-of-order observation changed evidence: %d", n)
	}
	now = now.Add(20 * time.Minute)
	got, err = s.GetReachability(ctx, f.alice, f.shared.ID)
	if err != nil || !got[0].Stale || got[0].State != "unknown" {
		t.Fatalf("old evidence looked current: %+v %v", got, err)
	}
	if err := s.db.Model(&store.Server{}).Where("id = ?", f.shared.ID).Update("host", "192.0.2.99").Error; err != nil {
		t.Fatal(err)
	}
	input.Host = "192.0.2.99"
	input.State = "unknown"
	input.Reason = "network_error"
	input.ObservedAt = now
	if err := s.SubmitReachability(ctx, f.alice, f.shared.ID, input); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetReachability(ctx, f.alice, f.shared.ID)
	if err != nil || got[0].LastKnownAt != nil || got[0].LastKnownState != "" {
		t.Fatalf("old host evidence moved to new host: %+v %v", got, err)
	}
	input.Host = "wrong.example"
	if err := s.SubmitReachability(ctx, f.alice, f.shared.ID, input); err == nil {
		t.Fatal("accepted another destination")
	}
}
