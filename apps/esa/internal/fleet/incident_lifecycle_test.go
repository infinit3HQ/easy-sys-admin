package fleet

import (
	"context"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func lifecycleServer(t *testing.T, s *Service, a Actor, name string) store.Server {
	t.Helper()
	srv := store.Server{AccountID: a.AccountID, Name: name, Host: "10.0.6.1", Port: 22, Username: "esa", Enrollment: "pending",
		ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	return srv
}

func incidentState(t *testing.T, s *Service, a Actor, id int64) IncidentView {
	t.Helper()
	v, err := s.GetIncident(context.Background(), a, id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNoticeResolvesOnlyOnFreshEvidenceAndRecurs(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "lifecycle")
	srv := lifecycleServer(t, s, a, "web")
	s.openIncident(ctx, a.AccountID, srv.ID, string(AlertSecurityUpdate), "", "openssl pending 9 days", true)
	list, _ := s.ListIncidents(ctx, a, IncidentFilter{})
	id := list[0].ID

	s.resolveIncident(ctx, a.AccountID, srv.ID, string(AlertSecurityUpdate), "")
	if v := incidentState(t, s, a, id); v.State != "resolved" || v.ResolvedAt == nil {
		t.Fatalf("after a fresh check: %+v", v.State)
	}
	if list, _ := s.ListIncidents(ctx, a, IncidentFilter{}); len(list) != 0 {
		t.Fatal("a resolved notice is not on the review list")
	}
	if !s.openIncident(ctx, a.AccountID, srv.ID, string(AlertSecurityUpdate), "", "openssl pending 9 days", false) {
		t.Fatal("a recurrence must open the notice again")
	}
	v := incidentState(t, s, a, id)
	if v.State != "open" || v.Recurrences != 1 {
		t.Fatalf("recurrence: %s %d", v.State, v.Recurrences)
	}
	kinds := []string{}
	for _, e := range v.Events {
		kinds = append(kinds, e.Kind)
	}
	if len(kinds) != 3 || kinds[0] != eventOpened || kinds[1] != eventResolved || kinds[2] != eventRecurred {
		t.Fatalf("history: %v", kinds)
	}
}

func TestUnreachableServerKeepsSecurityFindings(t *testing.T) {
	blind := blindKinds(&store.ServerState{Status: string(StatusUnreachable)})
	if !blind[AlertSecurityUpdate] || !blind[AlertExposure] {
		t.Fatal("an unreachable server must not resolve security findings")
	}
	notReady := blindKinds(&store.ServerState{Status: "ok", Snapshot: store.JSON(`{}`), Exposure: store.MustJSON(Exposure{Ready: false})})
	if notReady[AlertSecurityUpdate] || !notReady[AlertExposure] {
		t.Fatalf("a report that is not ready keeps exposure findings only: %v", notReady)
	}
	fresh := blindKinds(&store.ServerState{Status: "ok", Snapshot: store.JSON(`{}`), Exposure: store.MustJSON(Exposure{Ready: true})})
	if len(fresh) != 0 {
		t.Fatalf("fresh data judges everything: %v", fresh)
	}
}

func TestAcceptedRiskAndSnoozeRules(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "accept")
	srv := lifecycleServer(t, s, a, "db")
	s.openIncident(ctx, a.AccountID, srv.ID, string(AlertNewPort), "5432/tcp", "db: new socket 5432/tcp", true)
	list, _ := s.ListIncidents(ctx, a, IncidentFilter{})
	id := list[0].ID

	if _, err := s.AcceptIncident(ctx, a, id, s.now().Add(91*24*time.Hour), "replica traffic"); err == nil {
		t.Fatal("an acceptance longer than 90 days was allowed")
	}
	if _, err := s.AcceptIncident(ctx, a, id, s.now().Add(30*24*time.Hour), " "); err == nil {
		t.Fatal("an acceptance needs a reason")
	}
	v, err := s.AcceptIncident(ctx, a, id, s.now().Add(30*24*time.Hour), "replica traffic")
	if err != nil || v.State != "accepted" || v.Reason != "replica traffic" {
		t.Fatalf("accept: %+v %v", v.State, err)
	}
	if s.openIncident(ctx, a.AccountID, srv.ID, string(AlertNewPort), "5432/tcp", "db: new socket 5432/tcp", true) {
		t.Fatal("the same accepted condition reopened the notice")
	}
	if !s.openIncident(ctx, a.AccountID, srv.ID, string(AlertNewPort), "5432/tcp", "db: new socket 5432/tcp from postgres in a new container", true) {
		t.Fatal("a changed condition must end the acceptance")
	}
	if _, err := s.SnoozeIncident(ctx, a, id, s.now().Add(24*time.Hour), "look tomorrow"); err != nil {
		t.Fatal(err)
	}
	if !s.openIncident(ctx, a.AccountID, srv.ID, string(AlertNewPort), "5432/tcp", "db: new socket 5432/tcp from postgres in a new container", true) {
		t.Fatal("a new occurrence must break a snooze")
	}
	if _, err := s.SnoozeIncident(ctx, a, id, s.now().Add(time.Minute), "brief"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&store.SecurityIncident{}).Where("id = ?", id).Update("snooze_until", s.now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if v := incidentState(t, s, a, id); v.State != "open" {
		t.Fatalf("an ended snooze must return the notice, got %s", v.State)
	}
	v, err = s.AcknowledgeIncident(ctx, a, id, "postgres replica, expected")
	if err != nil || v.Note != "postgres replica, expected" {
		t.Fatalf("note: %+v %v", v.Note, err)
	}
}
