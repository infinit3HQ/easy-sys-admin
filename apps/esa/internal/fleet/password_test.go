package fleet

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestPasswordSignUpAndSignIn(t *testing.T) {
	s := newTestService(t)
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

	// A Google account with the same address is a different user, so
	// neither can take over the other.
	g, err := s.SignIn(ctx, Identity{Issuer: "https://accounts.google.com", Subject: "1234", Email: "ana@example.com", Name: "Ana G"}, SourceWeb)
	if err != nil {
		t.Fatal(err)
	}
	if g.User.ID == res.User.ID || g.AccountID == res.AccountID {
		t.Fatal("a Google sign-in must not land in the password account")
	}
}
