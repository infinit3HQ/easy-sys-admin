package fleet

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/crypto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// newTestService connects to ESA_TEST_DATABASE_URL, wipes it, and migrates.
// The test is skipped when the variable is unset.
func newTestService(t *testing.T) *Service {
	t.Helper()
	dsn := os.Getenv("ESA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ESA_TEST_DATABASE_URL to a disposable PostgreSQL database to run this test")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, err := store.Open(ctx, dsn, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.DB().Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public`).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx, log); err != nil {
		t.Fatal(err)
	}
	box, _ := crypto.NewBox(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	u, _ := url.Parse("http://localhost:3000")
	return New(ctx, Deps{Store: st, Box: box, Pool: remote.NewPool(), PublicURL: u, Logger: log,
		Helpers: func(string) ([]byte, error) { return nil, errors.New("no helpers in tests") }})
}

func signIn(t *testing.T, s *Service, subject string) Actor {
	t.Helper()
	res, err := s.SignIn(context.Background(), Identity{Issuer: "test", Subject: subject, Email: subject + "@example.com", Name: subject}, SourceWeb)
	if err != nil {
		t.Fatal(err)
	}
	return Actor{UserID: res.User.ID, AccountID: res.AccountID, Role: RoleOwner, Source: SourceWeb}
}

// A token or session from one account must never see or change another
// account's servers, groups, runs, or alerts.
func TestAccountIsolation(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	alice, bob := signIn(t, s, "alice"), signIn(t, s, "bob")
	if alice.AccountID == bob.AccountID {
		t.Fatal("each new user gets their own account")
	}

	// Bob has a server, a group, a run, and a stored login.
	group, err := s.CreateGroup(ctx, bob, GroupInput{Name: "prod", Timezone: "UTC",
		Clean: CleanPolicy{Days: []string{"sun"}, Time: "03:00", JournalSize: "200M"}, Updates: UpdatePolicy{Days: []string{"sun"}, Time: "04:00"}})
	if err != nil {
		t.Fatal(err)
	}
	srv := store.Server{AccountID: bob.AccountID, GroupID: &group.ID, Name: "bob-1", Host: "10.0.0.1", Port: 22, Username: "esa",
		Enrollment: "ready", ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.storeLogin(ctx, s.store.Queries, srv.ID, remote.LoginAuth{User: "root", Password: "bobs-secret-password"}); err != nil {
		t.Fatal(err)
	}
	run, err := s.createRun(ctx, bob, bob.AccountID, RunMeasure, "manual", nil, []store.Server{srv}, RunOptions{}, "succeeded")
	if err != nil {
		t.Fatal(err)
	}

	notFound := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: want ErrNotFound for another account's data, got %v", name, err)
		}
	}
	_, err = s.GetServer(ctx, alice, srv.ID)
	notFound("GetServer", err)
	_, err = s.UpdateServer(ctx, alice, srv.ID, UpdateServerInput{Name: ptr("stolen")})
	notFound("UpdateServer", err)
	notFound("DeleteServer", s.DeleteServer(ctx, alice, srv.ID, false))
	_, err = s.GetGroup(ctx, alice, group.ID)
	notFound("GetGroup", err)
	notFound("DeleteGroup", s.DeleteGroup(ctx, alice, group.ID))
	_, err = s.GetRun(ctx, alice, run.ID)
	notFound("GetRun", err)
	_, err = s.Services(ctx, alice, srv.ID)
	notFound("Services", err)
	_, err = s.StartRun(ctx, alice, RunRequest{Kind: RunMeasure, ServerIDs: []int64{srv.ID}})
	notFound("StartRun on another account's server", err)
	_, err = s.StartRun(ctx, alice, RunRequest{Kind: RunMeasure, GroupID: &group.ID})
	notFound("StartRun on another account's group", err)
	if _, err := s.UpdateServer(ctx, bob, srv.ID, UpdateServerInput{GroupID: ptr(int64(999999))}); err == nil {
		t.Error("moving a server into a group that does not exist must fail")
	}

	ov, err := s.Overview(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Servers) != 0 || ov.Totals.Groups != 0 {
		t.Errorf("alice sees bob's fleet: %+v", ov.Totals)
	}
	runs, _ := s.ListRuns(ctx, alice, RunFilter{})
	if len(runs) != 0 {
		t.Errorf("alice sees %d of bob's runs", len(runs))
	}

	// Alice's API token is bound to her account, whatever she asks for.
	_, secret, err := s.CreateToken(ctx, alice, "cli", []Scope{ScopeRead, ScopeAdmin}, 0)
	if err != nil {
		t.Fatal(err)
	}
	tokActor, ok, err := s.AuthenticateToken(ctx, secret, SourceAPI)
	if err != nil || !ok || tokActor.AccountID != alice.AccountID {
		t.Fatalf("token actor = %+v, %v, %v", tokActor, ok, err)
	}
	_, err = s.GetServer(ctx, tokActor, srv.ID)
	notFound("GetServer with token", err)

	// Stored logins never appear in views.
	v, err := s.GetServer(ctx, bob, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !v.HasStoredLogin {
		t.Error("bob's server has a stored login")
	}
	if strings.Contains(fmt.Sprintf("%+v", v), "bobs-secret-password") {
		t.Error("server view contains the stored password")
	}
	var cred store.ServerCredential
	s.db.Where("server_id = ?", srv.ID).Take(&cred)
	if strings.Contains(cred.SecretEnc, "bobs-secret-password") {
		t.Error("password stored in plaintext")
	}
}

// Viewers can read but cannot change anything.
func TestViewerCannotChange(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "owner")
	viewer := owner
	viewer.Role = RoleViewer
	if _, err := s.CreateGroup(ctx, viewer, GroupInput{Name: "g", Timezone: "UTC"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer CreateGroup: %v", err)
	}
	if _, err := s.StartRun(ctx, viewer, RunRequest{Kind: RunMeasure, ServerIDs: []int64{1}}); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer StartRun: %v", err)
	}
	if _, err := s.Logs(ctx, viewer, 1, LogQuery{Unit: "ssh.service"}); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer Logs: %v", err)
	}
	if _, _, err := s.CreateToken(ctx, viewer, "x", []Scope{ScopeActions}, 0); err == nil {
		t.Error("viewer must not mint an actions token")
	}
	if _, err := s.Overview(ctx, viewer); err != nil {
		t.Errorf("viewer Overview: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }
