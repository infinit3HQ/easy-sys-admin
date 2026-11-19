package fleet

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// Another team that receives a share sees the runs of the shared group, not
// who inside the owning team asked for them, their notes, or the ids of
// servers that are not shared.
func TestReceiverDoesNotSeeOwnerDetails(t *testing.T) {
	f := newShareFixture(t)
	s, ctx := f.s, context.Background()
	f.accept(t, "view")
	run, err := s.StartRun(ctx, f.alice, RunRequest{Kind: RunMeasure, GroupID: &f.group.ID})
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	opts := `{"canaryServerIds":[` + itoa(f.shared.ID) + `,` + itoa(f.unshared.ID) + `],"override":{"checks":["web"],"reason":"x","by":` + itoa(f.alice.UserID) + `}}`
	if err := s.db.Model(&store.Run{}).Where("id = ?", run.ID).Updates(map[string]any{
		"options": store.JSON(opts), "gate_note": "private note", "gate_state": "waiting", "gate_by": f.alice.UserID,
	}).Error; err != nil {
		t.Fatal(err)
	}

	own, err := s.GetRun(ctx, f.alice, run.ID)
	if err != nil || own.RequestedBy == nil || own.Gate == nil || own.Gate.Note != "private note" || len(own.Options.CanaryServerIDs) != 2 {
		t.Fatalf("owner view lost its details: %+v %v", own, err)
	}
	got, err := s.GetRun(ctx, f.bob, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestedBy != nil {
		t.Errorf("receiver sees who asked: %v", *got.RequestedBy)
	}
	if got.Gate != nil && got.Gate.Note != "" {
		t.Errorf("receiver sees the owner's note: %q", got.Gate.Note)
	}
	if slices.Contains(got.Options.CanaryServerIDs, f.unshared.ID) {
		t.Errorf("receiver sees an unshared server id: %v", got.Options.CanaryServerIDs)
	}
	if got.Options.Override != nil && got.Options.Override.By != 0 {
		t.Errorf("receiver sees who overrode a check: %d", got.Options.Override.By)
	}
}

// The owner's activity shows that someone on the receiving team acted, not
// who they are.
func TestActivityHidesReceiverIdentity(t *testing.T) {
	f := newShareFixture(t)
	s, ctx := f.s, context.Background()
	f.accept(t, "operate")
	if _, err := s.StartRun(ctx, f.bob, RunRequest{Kind: RunMeasure, GroupID: &f.group.ID}); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	page, err := s.Activity(ctx, f.alice, 100)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, e := range page.Entries {
		if e.Action != "run.start" {
			continue
		}
		seen = true
		if strings.Contains(strings.ToLower(e.UserName), "bob") || strings.Contains(e.UserName, "@") {
			t.Errorf("owner sees the receiver's identity: %q", e.UserName)
		}
	}
	if !seen {
		t.Fatal("no run.start entry from the receiving team")
	}
}

// Accepting a risk, approving a baseline and changing what protects the
// control plane's own host are approvals. An agent or a script cannot make
// them, whatever its scopes.
func TestApprovalsAreForPeopleOnly(t *testing.T) {
	f := newRoleFixture(t)
	s, ctx := f.s, context.Background()
	tok := tokenActor(f.owner, ScopeRead, ScopeAdmin, ScopeActions)
	wantPerson := func(what string, err error) {
		t.Helper()
		var v *ValidationError
		if !errors.As(err, &v) || !strings.Contains(v.Message, "only a person") {
			t.Errorf("%s by a token: want a person-only refusal, got %v", what, err)
		}
	}
	wantPerson("key baseline", s.ApproveKeyBaseline(ctx, tok, f.gSrv.ID, "ok"))
	wantPerson("access baseline", s.ApproveAccessBaseline(ctx, tok, f.gSrv.ID, "ok"))
	_, err := s.CreateKeyException(ctx, tok, f.gSrv.ID, "root", "SHA256:x", "ok", s.now().Add(24*3600e9))
	wantPerson("key exception", err)
	_, err = s.CreateVulnException(ctx, tok, f.gSrv.ID, "CVE-2026-0001", "openssl", "ok", s.now().Add(24*3600e9))
	wantPerson("vulnerability exception", err)
	off := false
	_, err = s.UpdateServer(ctx, tok, f.gSrv.ID, UpdateServerInput{HostsControlPlane: &off})
	wantPerson("control plane host flag", err)

	// A person is not refused for that reason.
	var v *ValidationError
	if err := s.ApproveKeyBaseline(ctx, f.owner, f.gSrv.ID, "ok"); errors.As(err, &v) && strings.Contains(v.Message, "only a person") {
		t.Errorf("a person was refused as a token: %v", err)
	}
	if _, err := s.UpdateServer(ctx, f.owner, f.gSrv.ID, UpdateServerInput{HostsControlPlane: &off}); err != nil {
		t.Errorf("a person changes the control plane host flag: %v", err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// A team that receives a share sees a notice, not what the owning team wrote
// about it.
func TestReceiverDoesNotSeeNoticeNotes(t *testing.T) {
	f := newShareFixture(t)
	s, ctx := f.s, context.Background()
	f.accept(t, "view")
	inc := store.SecurityIncident{AccountID: f.alice.AccountID, ServerID: f.shared.ID, Kind: "exposure", Subject: "port-22",
		State: "open", Message: "SSH is open", Note: "call Dana", SnoozeReason: "waiting on vendor", FirstSeen: s.now(), LastSeen: s.now()}
	if err := s.db.Omit("Account", "Server", "Assignee").Create(&inc).Error; err != nil {
		t.Fatal(err)
	}
	got, err := s.GetIncident(ctx, f.bob, inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Note != "" || got.Reason != "" {
		t.Errorf("receiver sees the owner's notes: %q %q", got.Note, got.Reason)
	}
	own, err := s.GetIncident(ctx, f.alice, inc.ID)
	if err != nil || own.Note != "call Dana" {
		t.Errorf("owner view = %+v, %v", own, err)
	}
}
