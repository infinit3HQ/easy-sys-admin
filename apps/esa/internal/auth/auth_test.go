package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/crypto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestLoopbackRequest(t *testing.T) {
	cases := []struct {
		host, fwd string
		want      bool
	}{
		{"localhost:8080", "", true},
		{"127.0.0.1:8080", "localhost:3000", true},
		{"[::1]:8080", "[::1]", true},
		{"LOCALHOST", "", true},
		{"127.0.0.1:8080", "evil.example.com", false},  // DNS rebinding through the web proxy
		{"127.0.0.1:8080", "192.168.1.20:3000", false}, // LAN through the web proxy
		{"127.0.0.1:8080", "localhost, evil.example.com", false},
		{"esa.example.com", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/auth/dev-login", nil)
		r.Host = c.host
		if c.fwd != "" {
			r.Header.Set("X-Forwarded-Host", c.fwd)
		}
		if got := loopbackRequest(r); got != c.want {
			t.Errorf("Host %q, X-Forwarded-Host %q: %v, want %v", c.host, c.fwd, got, c.want)
		}
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("POST", "/auth/password/login", nil)
	r.RemoteAddr = "10.0.0.5:4321"
	if got := clientIP(r); got != "10.0.0.5" {
		t.Errorf("no header: %q", got)
	}
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 203.0.113.9")
	if got := clientIP(r); got != "203.0.113.9" {
		t.Errorf("forged first entry must be ignored: %q", got)
	}
	r.Header.Add("X-Forwarded-For", "198.51.100.1")
	if got := clientIP(r); got != "198.51.100.1" {
		t.Errorf("several headers: %q", got)
	}
}

func TestLoginLimits(t *testing.T) {
	l := newLoginLimits()
	for range 5 {
		l.fail("ana@example.com", "1.1.1.1")
	}
	if got := l.blocked("ana@example.com", "1.1.1.1"); got != "locked" {
		t.Fatalf("guesser after 5 failures: %q", got)
	}
	// The owner on another address is not locked out by the guesser.
	if got := l.blocked("ana@example.com", "2.2.2.2"); got != "" {
		t.Fatalf("owner from another address: %q", got)
	}
	// One client trying many emails hits the per-client budget.
	for i := range 30 {
		l.fail(strings.Repeat("x", i)+"@example.com", "3.3.3.3")
	}
	if got := l.blocked("new@example.com", "3.3.3.3"); got != "rate_limited" {
		t.Fatalf("client after 30 failures: %q", got)
	}
	l.succeed("ana@example.com", "1.1.1.1")
	if got := l.blocked("ana@example.com", "1.1.1.1"); got != "" {
		t.Fatalf("after a successful sign-in: %q", got)
	}
}

// newTestStore migrates a fresh esa_auth_test schema in
// ESA_TEST_DATABASE_URL. It uses its own schema so it can run at the same
// time as the fleet tests, which reset the public schema.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("ESA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ESA_TEST_DATABASE_URL to a disposable PostgreSQL database to run this test")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	setup, err := store.Open(ctx, dsn, log)
	if err != nil {
		t.Fatal(err)
	}
	err = setup.DB().Exec(`DROP SCHEMA IF EXISTS esa_auth_test CASCADE; CREATE SCHEMA esa_auth_test`).Error
	setup.Close()
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", "esa_auth_test")
	u.RawQuery = q.Encode()
	st, err := store.Open(ctx, u.String(), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx, log); err != nil {
		t.Fatal(err)
	}
	return st
}

func newTestFleet(st *store.Store, allowed ...string) *fleet.Service {
	box, _ := crypto.NewBox(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	u, _ := url.Parse("http://localhost:3000")
	return fleet.New(context.Background(), fleet.Deps{Store: st, Box: box, Pool: remote.NewPool(), PublicURL: u,
		AllowedEmails: allowed, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Helpers: func(string) ([]byte, error) { return nil, errors.New("no helpers in tests") }})
}

// start signs the user in and returns the session cookie. old, if set, is
// the cookie the browser already had.
func start(t *testing.T, m *Sessions, res fleet.SignInResult, old *http.Cookie) *http.Cookie {
	t.Helper()
	r := httptest.NewRequest("POST", "/auth/password/login", nil)
	if old != nil {
		r.AddCookie(old)
	}
	w := httptest.NewRecorder()
	if err := m.Start(context.Background(), w, r, res.User.ID, res.AccountID); err != nil {
		t.Fatal(err)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == m.CookieName() {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func resolves(t *testing.T, m *Sessions, c *http.Cookie) bool {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/v1/me", nil)
	r.AddCookie(c)
	_, ok, err := m.resolveActor(r)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestSessions(t *testing.T) {
	st := newTestStore(t)
	fl := newTestFleet(st)
	ctx := context.Background()
	ana, err := fl.SignIn(ctx, fleet.Identity{Issuer: "test", Subject: "ana", Email: "ana@example.com"}, fleet.SourceWeb)
	if err != nil {
		t.Fatal(err)
	}
	m := NewSessions(st, fl, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Now()
	m.now = func() time.Time { return now }

	// Signing in again replaces the session the browser carried.
	first := start(t, m, ana, nil)
	second := start(t, m, ana, first)
	if resolves(t, m, first) || !resolves(t, m, second) {
		t.Fatal("the old session must end when a new one starts")
	}

	// Idle for a day ends the session even before the absolute limit.
	now = now.Add(idleTTL + time.Minute)
	if resolves(t, m, second) {
		t.Fatal("idle session still valid")
	}

	// Sign out everywhere ends every session of the user.
	a, b := start(t, m, ana, nil), start(t, m, ana, nil)
	r := httptest.NewRequest("POST", "/auth/logout-all", nil)
	r.AddCookie(a)
	w := httptest.NewRecorder()
	m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := m.EndAll(r.Context(), w); err != nil {
			t.Fatal(err)
		}
	})).ServeHTTP(w, r)
	if resolves(t, m, a) || resolves(t, m, b) {
		t.Fatal("sessions survived logout-all")
	}
	if c := w.Result().Cookies(); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Fatalf("logout-all must clear the cookie: %+v", c)
	}

	// Taking someone off the allow list ends their session.
	c := start(t, m, ana, nil)
	m.fleet = newTestFleet(st, "someone-else@example.com")
	if resolves(t, m, c) {
		t.Fatal("session of a user no longer allowed")
	}
	m.fleet = newTestFleet(st, "ana@example.com")
	if resolves(t, m, c) {
		t.Fatal("a refused session must be deleted, not just skipped")
	}
}
