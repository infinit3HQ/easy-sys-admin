package fleet

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIncidentAssignmentIsolationAndMemberRemoval(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	alice, bob, outsider := signIn(t, s, "assign-owner"), signIn(t, s, "assign-viewer"), signIn(t, s, "assign-outsider")
	org, err := s.CreateOrganization(ctx, alice, "Assignments")
	if err != nil {
		t.Fatal(err)
	}
	owner := inTeam(alice, org)
	viewer := inTeam(bob, joinTeam(t, s, owner, bob, RoleViewer))
	srv := lifecycleServer(t, s, owner, "web")
	s.openIncident(ctx, owner.AccountID, srv.ID, string(AlertNewPort), "80/tcp", "web: socket", true)
	notices, err := s.ListIncidents(ctx, owner, IncidentFilter{})
	if err != nil || len(notices) != 1 {
		t.Fatalf("notices: %v %v", notices, err)
	}
	id := notices[0].ID
	if _, err := s.AssignIncident(ctx, viewer, id, &bob.UserID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer assigned: %v", err)
	}
	if _, err := s.AssignIncident(ctx, outsider, id, &outsider.UserID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign notice: %v", err)
	}
	if _, err := s.AssignIncident(ctx, owner, id, &outsider.UserID); err == nil {
		t.Fatal("assigned a nonmember")
	}
	token := owner
	token.TokenID = 1
	token.Scopes = []Scope{ScopeRead}
	if _, err := s.AssignIncident(ctx, token, id, &bob.UserID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("read token assigned: %v", err)
	}
	got, err := s.AssignIncident(ctx, owner, id, &bob.UserID)
	if err != nil || got.AssigneeID == nil || *got.AssigneeID != bob.UserID || got.AssigneeName == "" || got.State != "open" {
		t.Fatalf("assignment: %+v %v", got, err)
	}
	if len(got.Events) != 2 || got.Events[1].Kind != "assigned" {
		t.Fatalf("history: %+v", got.Events)
	}
	got, err = s.AssignIncident(ctx, owner, id, &bob.UserID)
	if err != nil || len(got.Events) != 2 {
		t.Fatal("idempotent assignment added history")
	}
	if err := s.RemoveMember(ctx, owner, bob.UserID); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetIncident(ctx, owner, id)
	if err != nil || got.AssigneeID != nil || len(got.Events) != 3 || got.Events[2].Kind != "unassigned" {
		t.Fatalf("member removal: %+v %v", got, err)
	}
	if n := auditCount(t, s, owner.AccountID, "incident.assign", 0); n != 1 {
		t.Fatalf("assignment audit count: %d", n)
	}
}

func TestSharedIncidentHidesAssigneeAndRefusesAssignment(t *testing.T) {
	f := newShareFixture(t)
	f.accept(t, "operate")
	ctx := context.Background()
	f.s.openIncident(ctx, f.alice.AccountID, f.shared.ID, string(AlertNewPort), "443/tcp", "shared socket", true)
	list, err := f.s.ListIncidents(ctx, f.alice, IncidentFilter{})
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	id := list[0].ID
	if _, err := f.s.AssignIncident(ctx, f.alice, id, &f.alice.UserID); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.GetIncident(ctx, f.bob, id)
	if err != nil || got.AssigneeID != nil || got.AssigneeName != "" || got.CanReview {
		t.Fatalf("shared assignee leaked: %+v %v", got, err)
	}
	if _, err := f.s.AssignIncident(ctx, f.bob, id, &f.bob.UserID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("shared write: %v", err)
	}
}

func TestCriticalNoticesCannotBeHeld(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "critical-holds")
	srv := lifecycleServer(t, s, a, "critical")
	for _, kind := range []string{string(AlertHostKey), string(AlertNewKey), "vuln", "access"} {
		s.openIncident(ctx, a.AccountID, srv.ID, kind, "subject", kind, true)
	}
	list, err := s.ListIncidents(ctx, a, IncidentFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, notice := range list {
		if notice.CanHold {
			t.Fatalf("critical notice can be held: %s", notice.Kind)
		}
		if _, err := s.SnoozeIncident(ctx, a, notice.ID, s.now().Add(time.Hour), "later"); err == nil {
			t.Fatal("critical snooze succeeded")
		}
		if _, err := s.AcceptIncident(ctx, a, notice.ID, s.now().Add(time.Hour), "accepted"); err == nil {
			t.Fatal("critical risk accepted")
		}
	}
}
