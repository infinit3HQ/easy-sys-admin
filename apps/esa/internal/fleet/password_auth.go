package fleet

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"unicode/utf8"

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
)

const (
	minPasswordLen = 10
	maxPasswordLen = 200
)

func normalizeEmail(raw string) (string, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil || addr.Name != "" || !strings.Contains(addr.Address, "@") || len(addr.Address) > 254 {
		return "", invalid("email", "enter an email address")
	}
	return strings.ToLower(addr.Address), nil
}

// SignUpPassword creates an email and password user with an account of
// their own.
func (s *Service) SignUpPassword(ctx context.Context, name, email, pw string) (SignInResult, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return SignInResult{}, err
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
	hash, err := password.Hash(pw)
	if err != nil {
		return SignInResult{}, err
	}

	var res SignInResult
	err = s.store.InTx(ctx, func(q *store.Queries) error {
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
		s.audit(ctx, q, a, "auth.sign_up", email, map[string]any{"method": "password"})
		return nil
	})
	return res, err
}

// SignInPassword checks an email and password.
func (s *Service) SignInPassword(ctx context.Context, email, pw string) (SignInResult, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return SignInResult{}, ErrBadCredentials
	}
	if len(pw) > maxPasswordLen*4 {
		return SignInResult{}, ErrBadCredentials
	}
	var u store.User
	err = s.db.WithContext(ctx).Where("issuer = ? AND subject = ?", LocalIssuer, email).Take(&u).Error
	if err != nil || u.PasswordHash == "" {
		_, _ = password.Check(password.Dummy, pw) // same cost as a real check
		return SignInResult{}, ErrBadCredentials
	}
	ok, err := password.Check(u.PasswordHash, pw)
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
