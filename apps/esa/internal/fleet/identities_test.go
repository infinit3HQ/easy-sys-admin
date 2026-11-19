package fleet

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// keyServer stores a ready server with an exposure summary of the given age
// and gaps, and the given key grants.
func keyServer(t *testing.T, s *Service, a Actor, name string, age time.Duration, gaps []string, grants map[string]string) store.Server {
	t.Helper()
	srv := store.Server{AccountID: a.AccountID, Name: name, Host: "10.0.7." + name[len(name)-1:], Port: 22, Username: "esa", Enrollment: "ready",
		ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	ex := Exposure{Ready: true, CheckedAt: s.now().Add(-age), PublicPorts: []string{}, Gaps: gaps}
	if err := s.db.Create(&store.ServerState{ServerID: srv.ID, Status: "ok", Reasons: store.JSON("[]"), Exposure: store.MustJSON(ex)}).Error; err != nil {
		t.Fatal(err)
	}
	for user, fp := range grants {
		row := store.KeyOccurrence{AccountID: a.AccountID, ServerID: srv.ID, User: user, Fingerprint: fp, KeyType: "ssh-ed25519", ObservedAt: s.now()}
		if err := s.db.Omit("Account", "Server").Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return srv
}

func TestOffboardingReportNeverClaimsMoreThanItSaw(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "offboard")
	keyServer(t, s, a, "web1", time.Minute, nil, map[string]string{"deploy": "SHA256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "root": "SHA256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	keyServer(t, s, a, "db2", 5*time.Hour, nil, map[string]string{"admin": "SHA256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	keyServer(t, s, a, "ci3", time.Minute, []string{"authorized_keys symlink"}, map[string]string{"ci": "SHA256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	if err := s.LabelKey(ctx, a, "SHA256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "Sam"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveIdentity(ctx, a, IdentityView{Name: "Sam", Kind: "person", Status: "leaving"}); err != nil {
		t.Fatal(err)
	}
	rep, err := s.Offboarding(ctx, a, "Sam")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Identity.Status != "leaving" || rep.Identity.Grants != 3 || rep.Identity.Servers != 2 {
		t.Fatalf("identity: %+v", rep.Identity)
	}
	if len(rep.Grants) != 3 || len(rep.SharedKeys) != 1 {
		t.Fatalf("grants %d shared %v", len(rep.Grants), rep.SharedKeys)
	}
	if rep.Complete || len(rep.Unchecked) != 2 {
		t.Fatalf("a stale server and a server with key gaps make it incomplete: %+v", rep.Unchecked)
	}
	md := rep.Markdown()
	for _, want := range []string{"Offboarding report: Sam", "**Incomplete.**", "does not prove access was removed", "Servers esa could not check", "SHA256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(strings.ToLower(md), "all access removed") || strings.Contains(strings.ToLower(md), "access was revoked") {
		t.Fatal("the report must never claim removal")
	}
}

func TestBaselineVersionsAndStaleRefusal(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "baseline")
	fresh := keyServer(t, s, a, "app1", time.Minute, nil, map[string]string{"deploy": "SHA256:ccccccccccccccccccccccccccccccccccccccccccc"})
	stale := keyServer(t, s, a, "app2", 5*time.Hour, nil, map[string]string{"deploy": "SHA256:ccccccccccccccccccccccccccccccccccccccccccc"})
	gappy := keyServer(t, s, a, "app3", time.Minute, []string{"custom AuthorizedKeysFile"}, nil)
	if err := s.ApproveKeyBaseline(ctx, a, stale.ID, ""); err == nil {
		t.Fatal("stale keys were approved")
	}
	if err := s.ApproveKeyBaseline(ctx, a, gappy.ID, ""); err == nil {
		t.Fatal("keys from unread files were approved")
	}
	if err := s.ApproveKeyBaseline(ctx, a, fresh.ID, "initial review"); err != nil {
		t.Fatal(err)
	}
	if err := s.ApproveKeyBaseline(ctx, a, fresh.ID, "after rotation"); err != nil {
		t.Fatal(err)
	}
	vs, err := s.BaselineVersions(ctx, a, fresh.ID)
	if err != nil || len(vs) != 2 || vs[0].Version != 2 || vs[0].Reason != "after rotation" || vs[1].Keys != 1 {
		t.Fatalf("versions: %+v %v", vs, err)
	}
}

func TestIdentityValidation(t *testing.T) {
	s := newTestService(t)
	a := signIn(t, s, "identity")
	if _, err := s.SaveIdentity(context.Background(), a, IdentityView{Name: "ci", Kind: "robot", Status: "active"}); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	v, err := s.SaveIdentity(context.Background(), a, IdentityView{Name: "  ci  bot ", Kind: "service", Status: "active", Note: "GitHub Actions deploy key"})
	if err != nil || v.Name != "ci bot" || v.Kind != "service" {
		t.Fatalf("save: %+v %v", v, err)
	}
	viewer := a
	viewer.Role = RoleViewer
	if _, err := s.SaveIdentity(context.Background(), viewer, IdentityView{Name: "x", Kind: "person", Status: "left"}); err == nil {
		t.Fatal("a viewer saved an owner")
	}
}

// The same key with a forced command on one login and none on another must
// show each login's own restriction: a report that marked root restricted
// would hide an open door.
func TestOffboardingKeepsRestrictionPerLogin(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "per-login")
	srv := keyServer(t, s, a, "box1", time.Minute, nil, map[string]string{"root": "SHA256:ddddddddddddddddddddddddddddddddddddddddddd"})
	opc := store.KeyOccurrence{AccountID: a.AccountID, ServerID: srv.ID, User: "opc", Fingerprint: "SHA256:ddddddddddddddddddddddddddddddddddddddddddd", KeyType: "ssh-rsa", HasCommand: true, ObservedAt: s.now()}
	if err := s.db.Omit("Account", "Server").Create(&opc).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.LabelKey(ctx, a, "SHA256:ddddddddddddddddddddddddddddddddddddddddddd", "Admin key"); err != nil {
		t.Fatal(err)
	}
	rep, err := s.Offboarding(ctx, a, "Admin key")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, g := range rep.Grants {
		got[g.User] = g.HasCommand
	}
	if got["root"] || !got["opc"] {
		t.Fatalf("restrictions per login: %v", got)
	}
}
