package fleet

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// playgroundAccess matches the shape read from the playground on 14 November
// 2026: SSH settings were read, password login was off, root login was
// without-password, sudoers was complete, and four login accounts were
// present. The names here are stand-ins. The live list was not copied.
func playgroundAccess() proto.AccessReport {
	return proto.AccessReport{
		SSHRead: true, PasswordAuth: false, PermitRootLogin: "without-password",
		PubkeyAuth: true, PermitEmptyPasswords: false,
		Users: []proto.LocalUser{
			{Name: "root", UID: 0, Shell: "/bin/bash", Password: "locked"},
			{Name: "ubuntu", UID: 1000, Shell: "/bin/bash", Password: "locked"},
			{Name: "esa", UID: 998, Shell: "/usr/sbin/nologin", Password: "locked"},
			{Name: "ops", UID: 1001, Shell: "/bin/bash", Password: "set"},
		},
		SudoRules: []proto.SudoRule{
			{Source: "/etc/sudoers.d/esa", Who: "esa", RunAs: "root", Commands: "/usr/local/lib/esa/esa-helper", NoPassword: true},
			{Source: "/etc/sudoers", Who: "%sudo", RunAs: "root", Commands: "ALL", NoPassword: true, Unrestricted: true},
			{Source: "/etc/sudoers", Who: "root", RunAs: "ALL", Commands: "ALL", Unrestricted: true},
			{Source: "/etc/sudoers.d/90-cloud-init-users", Who: "ubuntu", RunAs: "root", Commands: "ALL", NoPassword: true, Unrestricted: true},
		},
	}
}

func TestAccessBaselineUsesTheRecordedCheck(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "access-base")
	srv := addTestServer(t, s, a, nil, "mail")
	facts := accessFacts(playgroundAccess(), s.now())
	if err := s.db.Create(&store.ServerState{ServerID: srv.ID, Status: "ok", Reasons: store.JSON("[]"), AccessFacts: store.MustJSON(facts)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveAccessBaseline(ctx, a, srv.ID, ""); err == nil {
		t.Fatal("an empty reason was accepted")
	}
	partial := facts
	partial.SudoIncomplete = true
	if err := s.db.Model(&store.ServerState{}).Where("server_id = ?", srv.ID).Update("access_facts", store.MustJSON(partial)).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveAccessBaseline(ctx, a, srv.ID, "checked the playground"); err == nil {
		t.Fatal("a partial sudoers read was approved")
	}
	stale := facts
	stale.ObservedAt = s.now().Add(-3 * time.Hour)
	if err := s.db.Model(&store.ServerState{}).Where("server_id = ?", srv.ID).Update("access_facts", store.MustJSON(stale)).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveAccessBaseline(ctx, a, srv.ID, "checked the playground"); err == nil {
		t.Fatal("a stale check was approved")
	}
	if err := s.db.Model(&store.ServerState{}).Where("server_id = ?", srv.ID).Update("access_facts", store.MustJSON(facts)).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveAccessBaseline(ctx, a, srv.ID, "checked the playground"); err != nil {
		t.Fatal(err)
	}

	changed := playgroundAccess()
	changed.PasswordAuth = true
	changed.Users = append(changed.Users, proto.LocalUser{Name: "extra", UID: 1002, Shell: "/bin/bash", Password: "set"})
	changed.SudoRules[0].Commands = "ALL"
	cur := accessFacts(changed, s.now())
	drifts := compareAccess(srv.Name, facts, cur)
	blob := ""
	for _, d := range drifts {
		blob += d.Message + "\n"
	}
	if strings.Contains(strings.ToLower(blob), "revok") {
		t.Fatal(blob)
	}
	if !strings.Contains(blob, "password login changed") || !strings.Contains(blob, "login extra") || !strings.Contains(blob, "sudo rule was not in the approved list") {
		t.Fatalf("drift: %s", blob)
	}
	s.reportAccessDrift(ctx, srv, cur)
	var n int64
	if err := s.db.Model(&store.SecurityIncident{}).Where("server_id = ? AND kind = ?", srv.ID, "access").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n < 3 {
		t.Fatalf("incidents: %d", n)
	}
}

func TestAccessChangeIsSentDuringMaintenance(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "access-send")
	srv := addTestServer(t, s, a, nil, "mail")
	facts := accessFacts(playgroundAccess(), s.now())
	if err := s.db.Create(&store.ServerState{ServerID: srv.ID, Status: "ok", Reasons: store.JSON("[]"), AccessFacts: store.MustJSON(facts)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveAccessBaseline(ctx, a, srv.ID, "checked"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateChannel(ctx, a, "desk", "webhook", ChannelConfig{URL: "https://hook.example.com/access"}); err != nil {
		t.Fatal(err)
	}
	note := &countNotifier{}
	s.notifier = note
	win := store.MaintenanceWindow{AccountID: a.AccountID, Reason: "work", StartsAt: s.now().Add(-time.Minute), EndsAt: s.now().Add(time.Hour), CreatedBy: a.UserID}
	if err := s.db.Omit("Account").Create(&win).Error; err != nil {
		t.Fatal(err)
	}
	cur := facts
	cur.PasswordAuth = true
	s.reportAccessDrift(ctx, srv, cur)
	deadline := time.Now().Add(2 * time.Second)
	for note.n.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if note.n.Load() != 1 {
		t.Fatalf("access sends during maintenance: %d", note.n.Load())
	}
	s.reportAccessDrift(ctx, srv, cur)
	if note.n.Load() != 1 {
		t.Fatal("the same access change was sent again")
	}
	s.reportAccessDrift(ctx, srv, facts)
	var open int64
	if err := s.db.Model(&store.SecurityIncident{}).Where("server_id = ? AND kind = ? AND state <> ?", srv.ID, "access", "resolved").Count(&open).Error; err != nil {
		t.Fatal(err)
	}
	if open != 0 {
		t.Fatal("a matching check left the notice open")
	}
	cur.SudoIncomplete = true
	s.reportAccessDrift(ctx, srv, cur)
	s.reportAccessDrift(ctx, srv, cur)
	cur.SudoIncomplete = false
	s.reportAccessDrift(ctx, srv, cur)
	partial := cur
	partial.SudoIncomplete = true
	s.reportAccessDrift(ctx, srv, partial)
	if err := s.db.Model(&store.SecurityIncident{}).Where("server_id = ? AND kind = ? AND state <> ?", srv.ID, "access", "resolved").Count(&open).Error; err != nil {
		t.Fatal(err)
	}
	if open != 1 {
		t.Fatalf("a partial check changed the notice: %d open", open)
	}
}
