package fleet

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/password"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// LocalIssuer marks email and password accounts. Accounts from Google or
// another provider have that provider's issuer, so an email and password
// account can never take over a Google account with the same address, or
// the other way round.
const LocalIssuer = "local"

var (
	// ErrBadCredentials hides whether the email exists.
	ErrBadCredentials = errors.New("fleet: wrong email or password")
	// ErrEmailTaken means an email and password account already uses the address.
	ErrEmailTaken = errors.New("fleet: an account with this email already exists")
	// ErrSignupClosed means sign-up is off and the install already has users.
	ErrSignupClosed = errors.New("fleet: sign-up is closed")
)

const (
	minPasswordLen = 10
	maxPasswordLen = 200
)

// NormalizeEmail returns the address in the form accounts are stored and
// matched in (lowercase, no display name), or a validation error.
func NormalizeEmail(raw string) (string, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil || addr.Name != "" || !strings.Contains(addr.Address, "@") || len(addr.Address) > 254 {
		return "", invalid("email", "enter an email address")
	}
	return strings.ToLower(addr.Address), nil
}

// SignupOpen reports whether a new email and password account can be
// created now: sign-up is on, or the database has no users yet (the first
// user bootstraps the install).
func (s *Service) SignupOpen(ctx context.Context) (bool, error) {
	if s.signup {
		return true, nil
	}
	exists, err := hasUsers(s.db.WithContext(ctx))
	return !exists, err
}

func hasUsers(db *gorm.DB) (bool, error) {
	var exists bool
	err := db.Raw("SELECT EXISTS (SELECT 1 FROM users)").Scan(&exists).Error
	return exists, err
}

// SignUpPassword creates an email and password user with an account of
// their own. When sign-up is off it only succeeds for the first user.
func (s *Service) SignUpPassword(ctx context.Context, name, email, pw string) (SignInResult, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return SignInResult{}, err
	}
	if !s.signup {
		// Refuse early, before the allow list or a taken email can be probed.
		if open, err := s.SignupOpen(ctx); err != nil {
			return SignInResult{}, err
		} else if !open {
			return SignInResult{}, ErrSignupClosed
		}
	}
	if len(s.allowed) > 0 && !s.allowed[email] {
		return SignInResult{}, ErrSignInNotAllowed
	}
	if n := utf8.RuneCountInString(pw); n < minPasswordLen || n > maxPasswordLen {
		return SignInResult{}, invalid("password", "use %d to %d characters", minPasswordLen, maxPasswordLen)
	}
	if strings.EqualFold(pw, email) {
		return SignInResult{}, invalid("password", "must not be your email address")
	}
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > 100 {
		return SignInResult{}, invalid("name", "use at most 100 characters")
	}
	hash, err := password.Hash(ctx, pw)
	if err != nil {
		return SignInResult{}, err
	}

	var res SignInResult
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		detail := map[string]any{"method": "password"}
		if !s.signup {
			// Bootstrap. The lock blocks user inserts from every path until
			// this transaction ends, so two first sign-ups cannot both pass.
			if err := q.Raw().Exec("LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
				return err
			}
			exists, err := hasUsers(q.Raw())
			if err != nil {
				return err
			}
			if exists {
				return ErrSignupClosed
			}
			detail["bootstrap"] = true
		}
		now := s.now()
		u := store.User{Issuer: LocalIssuer, Subject: email, Email: email, Name: name, PasswordHash: hash, CreatedAt: now, LastLoginAt: now}
		r := q.Raw().Clauses(clause.OnConflict{DoNothing: true}).Create(&u)
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected == 0 {
			return ErrEmailTaken
		}
		acc, err := q.CreateAccount(ctx, defaultAccountName(u), now)
		if err != nil {
			return err
		}
		if err := q.AddMembership(ctx, acc.ID, u.ID, string(RoleOwner), now); err != nil {
			return err
		}
		res = SignInResult{User: u, AccountID: acc.ID}
		a := Actor{UserID: u.ID, AccountID: acc.ID, Role: RoleOwner, Source: SourceWeb}
		s.audit(ctx, q, a, "account.create", acc.Name, nil)
		s.audit(ctx, q, a, "auth.sign_up", email, detail)
		return nil
	})
	return res, err
}

// SignInPassword checks an email and password. It returns password.ErrBusy
// (or the context's error) when the server is too busy to check.
func (s *Service) SignInPassword(ctx context.Context, email, pw string) (SignInResult, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return SignInResult{}, ErrBadCredentials
	}
	if utf8.RuneCountInString(pw) > maxPasswordLen {
		return SignInResult{}, ErrBadCredentials // no account has a longer one
	}
	var u store.User
	err = s.db.WithContext(ctx).Where("issuer = ? AND subject = ?", LocalIssuer, email).Take(&u).Error
	if err != nil || u.PasswordHash == "" {
		// Same cost as a real check.
		if _, err := password.Check(ctx, password.Dummy, pw); busy(ctx, err) {
			return SignInResult{}, err
		}
		return SignInResult{}, ErrBadCredentials
	}
	ok, err := password.Check(ctx, u.PasswordHash, pw)
	if busy(ctx, err) {
		return SignInResult{}, err
	}
	if err != nil || !ok {
		return SignInResult{}, ErrBadCredentials
	}
	if len(s.allowed) > 0 && !s.allowed[email] {
		return SignInResult{}, ErrSignInNotAllowed
	}
	s.db.WithContext(ctx).Model(&store.User{}).Where("id = ?", u.ID).Update("last_login_at", s.now())
	m, err := s.FirstMembership(ctx, u.ID)
	if err != nil {
		return SignInResult{}, err
	}
	s.audit(ctx, nil, Actor{UserID: u.ID, AccountID: m.Account.ID, Source: SourceWeb}, "auth.sign_in", email, map[string]any{"method": "password"})
	return SignInResult{User: u, AccountID: m.Account.ID}, nil
}

// busy reports whether a password check failed for lack of capacity rather
// than a wrong password.
func busy(ctx context.Context, err error) bool {
	return errors.Is(err, password.ErrBusy) || (err != nil && ctx.Err() != nil)
}
