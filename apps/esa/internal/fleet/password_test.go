package fleet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestPasswordSignUpAndSignIn(t *testing.T) {
	s := newTestService(t)
	s.signup = true
	ctx := context.Background()

	if _, err := s.SignUpPassword(ctx, "Ana", "ana@example.com", "short"); err == nil {
		t.Fatal("a short password was accepted")
	}
	if _, err := s.SignUpPassword(ctx, "Ana", "not an email", "long enough password"); err == nil {
		t.Fatal("a bad email was accepted")
	}
	res, err := s.SignUpPassword(ctx, "Ana", "Ana@Example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if res.AccountID == 0 || res.User.Email != "ana@example.com" {
		t.Fatalf("sign up result %+v", res)
	}
	if _, err := s.SignUpPassword(ctx, "Eve", "ana@example.com", "another long password"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("second sign up with the same email: %v", err)
	}

	var u store.User
	s.db.Where("id = ?", res.User.ID).Take(&u)
	if !strings.HasPrefix(u.PasswordHash, "$argon2id$") || strings.Contains(u.PasswordHash, "correct horse") {
		t.Fatalf("password stored as %q", u.PasswordHash)
	}

	if _, err := s.SignInPassword(ctx, "ana@example.com", "wrong password here"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := s.SignInPassword(ctx, "nobody@example.com", "correct horse battery"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("unknown email must look like a wrong password: %v", err)
	}
	got, err := s.SignInPassword(ctx, " ANA@example.com ", "correct horse battery")
	if err != nil || got.User.ID != res.User.ID || got.AccountID != res.AccountID {
		t.Fatalf("sign in: %+v, %v", got, err)
	}

	// Google with the same address is the same user, and the address counts
	// as confirmed because Google already checked it.
	g, err := s.SignIn(ctx, Identity{Issuer: "https://accounts.google.com", Subject: "1234", Email: "ana@example.com", Name: "Ana G"}, SourceWeb)
	if err != nil {
		t.Fatal(err)
	}
	if g.User.ID != res.User.ID || g.AccountID != res.AccountID {
		t.Fatalf("google sign-in landed in user %d account %d, password user is %d account %d", g.User.ID, g.AccountID, res.User.ID, res.AccountID)
	}
	var gu store.User
	if err := s.db.Where("id = ?", g.User.ID).Take(&gu).Error; err != nil || !gu.EmailConfirmed {
		t.Fatalf("google sign-in confirmed=%v err=%v", gu.EmailConfirmed, err)
	}
	again, err := s.SignIn(ctx, Identity{Issuer: "https://accounts.google.com", Subject: "1234", Email: "ana@example.com", Name: "Ana G"}, SourceWeb)
	if err != nil || again.User.ID != res.User.ID {
		t.Fatalf("second google sign-in: %+v %v", again, err)
	}
}

func TestPasswordJoinsProviderAccount(t *testing.T) {
	s := newTestService(t)
	s.signup = true
	ctx := context.Background()
	g, err := s.SignIn(ctx, Identity{Issuer: "https://accounts.google.com", Subject: "1234", Email: "ana@example.com", Name: "Ana"}, SourceWeb)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SignUpPassword(ctx, "Ana", "ana@example.com", "correct horse battery"); !errors.Is(err, ErrProviderAccount) {
		t.Fatalf("password sign-up without mail: %v", err)
	}
	var n int64
	s.db.Model(&store.User{}).Count(&n)
	if n != 1 {
		t.Fatalf("%d users, want 1", n)
	}

	box := &mailBox{}
	s.authMail = box
	res, err := s.SignUpPassword(ctx, "Ana", "ana@example.com", "correct horse battery")
	if err != nil || !res.NeedsConfirm || res.User.ID != g.User.ID {
		t.Fatalf("sign up onto google user: %+v %v", res, err)
	}
	if _, err := s.SignInPassword(ctx, "ana@example.com", "correct horse battery"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("password before the link: %v", err)
	}
	if err := s.ConfirmEmail(ctx, mailToken(t, box.last)); err != nil {
		t.Fatal(err)
	}
	got, err := s.SignInPassword(ctx, "ana@example.com", "correct horse battery")
	if err != nil || got.User.ID != g.User.ID || got.AccountID != g.AccountID {
		t.Fatalf("password after confirm: %+v %v", got, err)
	}
	s.db.Model(&store.User{}).Count(&n)
	if n != 1 {
		t.Fatalf("%d users after confirm, want 1", n)
	}
}

// With sign-up off, exactly one password account can be created, by the
// first person to reach an empty database, even when requests race.
func TestPasswordBootstrap(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	if open, err := s.SignupOpen(ctx); err != nil || !open {
		t.Fatalf("empty database: open %v, %v", open, err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = s.SignUpPassword(ctx, "", fmt.Sprintf("u%d@example.com", i), "correct horse battery")
		})
	}
	wg.Wait()
	created := 0
	for _, err := range errs {
		switch {
		case err == nil:
			created++
		case !errors.Is(err, ErrSignupClosed):
			t.Errorf("racing sign-up: %v", err)
		}
	}
	if created != 1 {
		t.Fatalf("%d bootstrap accounts created, want 1", created)
	}
	if open, err := s.SignupOpen(ctx); err != nil || open {
		t.Fatalf("after bootstrap: open %v, %v", open, err)
	}
	// Closed sign-up does not reveal whether an address is taken.
	if _, err := s.SignUpPassword(ctx, "", "u0@example.com", "another long password"); !errors.Is(err, ErrSignupClosed) {
		t.Fatalf("sign-up after bootstrap: %v", err)
	}
}

func TestPasswordBootstrapAllowList(t *testing.T) {
	s := newTestService(t)
	s.allowed = map[string]bool{"owner@example.com": true}
	ctx := context.Background()
	if _, err := s.SignUpPassword(ctx, "", "eve@example.com", "correct horse battery"); !errors.Is(err, ErrSignInNotAllowed) {
		t.Fatalf("bootstrap with an address not on the list: %v", err)
	}
	if _, err := s.SignUpPassword(ctx, "", "Owner@Example.com", "correct horse battery"); err != nil {
		t.Fatalf("bootstrap with an allowed address: %v", err)
	}
}

func TestPasswordLengthCapAtSignIn(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	if _, err := s.SignInPassword(ctx, "ana@example.com", strings.Repeat("x", maxPasswordLen+1)); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("over-long password: %v", err)
	}
}

// Removing an address from the allow list disables that user's tokens.
func TestTokenAllowListAndExpiry(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	ana := signIn(t, s, "ana")

	for _, ttl := range []time.Duration{-24 * time.Hour, time.Hour, 366 * 24 * time.Hour} {
		var verr *ValidationError
		if _, _, err := s.CreateToken(ctx, ana, "bad", nil, ttl); !errors.As(err, &verr) || verr.Field != "expiresInDays" {
			t.Errorf("ttl %v: %v", ttl, err)
		}
	}
	view, secret, err := s.CreateToken(ctx, ana, "cli", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if view.ExpiresAt == nil || view.ExpiresAt.Sub(view.CreatedAt) != DefaultTokenTTL {
		t.Fatalf("default expiry %v", view.ExpiresAt)
	}

	if _, ok, err := s.AuthenticateToken(ctx, secret, SourceAPI); !ok || err != nil {
		t.Fatalf("token before allow list: %v, %v", ok, err)
	}
	s.allowed = map[string]bool{"someone-else@example.com": true}
	if _, ok, err := s.AuthenticateToken(ctx, secret, SourceAPI); ok || err != nil {
		t.Fatalf("token of a user no longer allowed: %v, %v", ok, err)
	}
	s.allowed = map[string]bool{"ana@example.com": true}
	if _, ok, err := s.AuthenticateToken(ctx, secret, SourceAPI); !ok || err != nil {
		t.Fatalf("token of an allowed user: %v, %v", ok, err)
	}
}

type mailBox struct{ last string }

func (b *mailBox) Send(_ context.Context, _, _, text, _ string) error {
	b.last = text
	return nil
}

func mailToken(t *testing.T, body string) string {
	t.Helper()
	const key = "token="
	i := strings.Index(body, key)
	if i < 0 {
		t.Fatalf("no token in %q", body)
	}
	token := body[i+len(key):]
	if n := strings.IndexAny(token, "&\n "); n >= 0 {
		token = token[:n]
	}
	return token
}

func TestAuthMailConfirmAndReset(t *testing.T) {
	s := newTestService(t)
	s.signup = true
	box := &mailBox{}
	s.authMail = box
	ctx := context.Background()

	res, err := s.SignUpPassword(ctx, "Ana", "ana@example.com", "correct horse battery")
	if err != nil || res.AccountID == 0 || !strings.Contains(box.last, "token=") {
		t.Fatalf("sign up = %+v, %v, mail %q", res, err, box.last)
	}
	if _, err := s.SignInPassword(ctx, "ana@example.com", "correct horse battery"); !errors.Is(err, ErrEmailUnconfirmed) {
		t.Fatalf("sign in before confirm: %v", err)
	}
	if err := s.ConfirmEmail(ctx, mailToken(t, box.last)); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmEmail(ctx, mailToken(t, box.last)); !errors.Is(err, ErrAuthToken) {
		t.Fatalf("second confirm: %v", err)
	}
	if _, err := s.SignInPassword(ctx, "ana@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	if err := s.RequestPasswordReset(ctx, "missing@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestPasswordReset(ctx, "ana@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetPassword(ctx, mailToken(t, box.last), "a different long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SignInPassword(ctx, "ana@example.com", "correct horse battery"); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("old password: %v", err)
	}
	if _, err := s.SignInPassword(ctx, "ana@example.com", "a different long password"); err != nil {
		t.Fatal(err)
	}
}
