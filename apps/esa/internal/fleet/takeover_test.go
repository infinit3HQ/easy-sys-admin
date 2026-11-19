package fleet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const (
	googleIssuer   = "https://accounts.google.com"
	attackerSecret = "attacker chosen password"
)

// seedLiveCredentials gives a user a session, an API token and a pending
// auth email token, the things an attacker would keep after a takeover.
func seedLiveCredentials(t *testing.T, s *Service, userID, accountID int64) {
	t.Helper()
	now := time.Now()
	rows := []any{
		&store.Session{TokenHash: []byte("session-" + time.Now().String()), UserID: userID, AccountID: accountID,
			CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour)},
		&store.APIToken{UserID: userID, AccountID: accountID, Name: "seed", TokenHash: []byte("token-" + time.Now().String()),
			Prefix: "esa_seed", CreatedAt: now},
		&store.AuthEmailToken{TokenHash: []byte("mail-" + time.Now().String()), UserID: userID, Purpose: "confirm",
			ExpiresAt: now.Add(time.Hour), CreatedAt: now},
	}
	for _, r := range rows {
		if err := s.db.Create(r).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func countRows(t *testing.T, s *Service, model any, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := s.db.Model(model).Where(where, args...).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// An attacker who signs up with a victim's address and a password of their
// own must not keep that password when the victim later signs in with Google.
func TestProviderSignInDropsUnconfirmedPassword(t *testing.T) {
	for _, withMail := range []bool{false, true} {
		name := "without mail"
		if withMail {
			name = "with mail"
		}
		t.Run(name, func(t *testing.T) {
			s := newTestService(t)
			s.signup = true
			if withMail {
				s.authMail = &mailBox{}
			}
			ctx := context.Background()

			planted, err := s.SignUpPassword(ctx, "Attacker", "victim@example.com", attackerSecret)
			if err != nil {
				t.Fatal(err)
			}
			seedLiveCredentials(t, s, planted.User.ID, planted.AccountID)

			victim, err := s.SignIn(ctx, Identity{Issuer: googleIssuer, Subject: "g-1", Email: "victim@example.com", Name: "Victim"}, SourceWeb)
			if err != nil {
				t.Fatal(err)
			}
			if victim.User.ID != planted.User.ID {
				t.Fatalf("victim joined user %d, want the existing row %d", victim.User.ID, planted.User.ID)
			}
			if _, err := s.SignInPassword(ctx, "victim@example.com", attackerSecret); !errors.Is(err, ErrBadCredentials) {
				t.Fatalf("attacker password after the victim signed in: %v", err)
			}
			if n := countRows(t, s, &store.Session{}, "user_id = ?", planted.User.ID); n != 0 {
				t.Fatalf("%d sessions survived the link", n)
			}
			if n := countRows(t, s, &store.APIToken{}, "user_id = ? AND revoked_at IS NULL", planted.User.ID); n != 0 {
				t.Fatalf("%d live API tokens survived the link", n)
			}
			if n := countRows(t, s, &store.AuthEmailToken{}, "user_id = ?", planted.User.ID); n != 0 {
				t.Fatalf("%d pending email tokens survived the link", n)
			}
		})
	}
}

// A password account that proved its address keeps its password when a
// provider with the same verified address signs in.
func TestProviderSignInKeepsConfirmedPassword(t *testing.T) {
	s := newTestService(t)
	s.signup = true
	box := &mailBox{}
	s.authMail = box
	ctx := context.Background()

	if _, err := s.SignUpPassword(ctx, "Ana", "ana@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmEmail(ctx, mailToken(t, box.last)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SignIn(ctx, Identity{Issuer: googleIssuer, Subject: "g-2", Email: "ana@example.com", Name: "Ana"}, SourceWeb); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SignInPassword(ctx, "ana@example.com", "correct horse battery"); err != nil {
		t.Fatalf("confirmed password lost after provider sign-in: %v", err)
	}
}

// "Add a password" must let the mailbox owner choose the password. The one
// typed on the sign-up form by whoever asked is never applied.
func TestAddPasswordLinkIgnoresRequesterPassword(t *testing.T) {
	s := newTestService(t)
	s.signup = true
	ctx := context.Background()
	g, err := s.SignIn(ctx, Identity{Issuer: googleIssuer, Subject: "g-3", Email: "ana@example.com", Name: "Ana"}, SourceWeb)
	if err != nil {
		t.Fatal(err)
	}
	box := &mailBox{}
	s.authMail = box
	res, err := s.SignUpPassword(ctx, "Attacker", "ana@example.com", attackerSecret)
	if err != nil || !res.NeedsConfirm {
		t.Fatalf("sign up onto google user: %+v %v", res, err)
	}
	token := mailToken(t, box.last)
	if err := s.ConfirmEmail(ctx, token); err == nil {
		// Opening the link as a confirmation must not install a password.
		if _, err := s.SignInPassword(ctx, "ana@example.com", attackerSecret); !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("requester password works after the confirm link: %v", err)
		}
		t.Fatal("a reset link was accepted as a confirmation link")
	}

	if err := s.ResetPassword(ctx, token, "owner chose this one"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SignInPassword(ctx, "ana@example.com", attackerSecret); !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("requester password after the owner set one: %v", err)
	}
	got, err := s.SignInPassword(ctx, "ana@example.com", "owner chose this one")
	if err != nil || got.User.ID != g.User.ID {
		t.Fatalf("owner password: %+v %v", got, err)
	}
}

// Sign-up onto an existing provider account sends at most three mails an
// hour to one address, like the forgot-password form.
func TestAddPasswordMailIsLimitedPerAddress(t *testing.T) {
	s := newTestService(t)
	s.signup = true
	ctx := context.Background()
	if _, err := s.SignIn(ctx, Identity{Issuer: googleIssuer, Subject: "g-4", Email: "ana@example.com", Name: "Ana"}, SourceWeb); err != nil {
		t.Fatal(err)
	}
	box := &countingMail{}
	s.authMail = box
	for i := 0; i < 6; i++ {
		if _, err := s.SignUpPassword(ctx, "Spam", "ana@example.com", attackerSecret); err != nil {
			t.Fatal(err)
		}
	}
	if box.sent != 3 {
		t.Fatalf("%d mails sent to one address in an hour, want 3", box.sent)
	}
}

// A password reset ends sessions and revokes API tokens, so a takeover does
// not outlive the owner recovering the account.
func TestResetPasswordRevokesTokens(t *testing.T) {
	s := newTestService(t)
	s.signup = true
	box := &mailBox{}
	s.authMail = box
	ctx := context.Background()

	res, err := s.SignUpPassword(ctx, "Ana", "ana@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmEmail(ctx, mailToken(t, box.last)); err != nil {
		t.Fatal(err)
	}
	seedLiveCredentials(t, s, res.User.ID, res.AccountID)
	if err := s.RequestPasswordReset(ctx, "ana@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetPassword(ctx, mailToken(t, box.last), "a different long password"); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, &store.APIToken{}, "user_id = ? AND revoked_at IS NULL", res.User.ID); n != 0 {
		t.Fatalf("%d live API tokens survived the reset", n)
	}
	if n := countRows(t, s, &store.Session{}, "user_id = ?", res.User.ID); n != 0 {
		t.Fatalf("%d sessions survived the reset", n)
	}
}

type countingMail struct{ sent int }

func (c *countingMail) Send(context.Context, string, string, string, string) error {
	c.sent++
	return nil
}

// Adding a password to a provider account is not a recovery. It keeps the
// user's sessions and API tokens, which no password ever opened.
func TestAddPasswordKeepsTokensOfProviderAccount(t *testing.T) {
	s := newTestService(t)
	s.signup = true
	ctx := context.Background()
	g, err := s.SignIn(ctx, Identity{Issuer: googleIssuer, Subject: "g-5", Email: "ana@example.com", Name: "Ana"}, SourceWeb)
	if err != nil {
		t.Fatal(err)
	}
	seedLiveCredentials(t, s, g.User.ID, g.AccountID)
	box := &mailBox{}
	s.authMail = box
	res, err := s.SignUpPassword(ctx, "Ana", "ana@example.com", "typed on the form")
	if err != nil || !res.AddPassword {
		t.Fatalf("sign up onto google user: %+v %v", res, err)
	}
	if err := s.ResetPassword(ctx, mailToken(t, box.last), "the owner picks this"); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, &store.APIToken{}, "user_id = ? AND revoked_at IS NULL", g.User.ID); n != 1 {
		t.Fatalf("%d live API tokens after adding a password, want 1", n)
	}
	if n := countRows(t, s, &store.Session{}, "user_id = ?", g.User.ID); n != 1 {
		t.Fatalf("%d sessions after adding a password, want 1", n)
	}
}

// With sign-up closed and no mail, the only password account is the one the
// operator made. A later Google sign-in must not take its password away.
func TestProviderSignInKeepsPasswordWhenNobodyCanPlantOne(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	boot, err := s.SignUpPassword(ctx, "Olga", "olga@example.com", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	seedLiveCredentials(t, s, boot.User.ID, boot.AccountID)
	if _, err := s.SignIn(ctx, Identity{Issuer: googleIssuer, Subject: "g-6", Email: "olga@example.com", Name: "Olga"}, SourceWeb); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SignInPassword(ctx, "olga@example.com", "correct horse battery"); err != nil {
		t.Fatalf("operator password lost: %v", err)
	}
	if n := countRows(t, s, &store.APIToken{}, "user_id = ? AND revoked_at IS NULL", boot.User.ID); n != 1 {
		t.Fatalf("%d live API tokens, want 1", n)
	}
}
