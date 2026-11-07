package fleet

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/password"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// LocalIssuer marks an account that started with email and password.
// A later Google sign-in with the same verified email joins that account.
const LocalIssuer = "local"

var (
	// ErrBadCredentials hides whether the email exists.
	ErrBadCredentials = errors.New("fleet: wrong email or password")
	// ErrEmailTaken means an email and password account already uses the address.
	ErrEmailTaken = errors.New("fleet: an account with this email already exists")
	// ErrProviderAccount means this address already signs in with Google or
	// another provider, and system mail is off so a password cannot be added
	// safely. The person should use that provider. It is the same account.
	ErrProviderAccount = errors.New("fleet: email already signs in with a provider")
	// ErrSignupClosed means sign-up is off and the install already has users.
	ErrSignupClosed = errors.New("fleet: sign-up is closed")
	// ErrEmailUnconfirmed means the password was right but the address has
	// not opened its confirmation link. Google accounts never hit this.
	ErrEmailUnconfirmed = errors.New("fleet: email is not confirmed")
	// ErrAuthToken means a confirmation or reset link is missing, used, or expired.
	ErrAuthToken = errors.New("fleet: auth email token is invalid")
	// ErrMailFailed means the account change was saved but the message was not sent.
	ErrMailFailed = errors.New("fleet: could not send auth email")
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

// SignUpPassword creates an email and password user with a personal space.
// When sign-up is off it only succeeds for the first user.
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
	if err := validPassword(pw, email); err != nil {
		return SignInResult{}, err
	}
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > 100 {
		return SignInResult{}, invalid("name", "use at most 100 characters")
	}
	hash, err := password.Hash(ctx, pw)
	if err != nil {
		return SignInResult{}, err
	}

	existing, findErr := userByEmail(s.db.WithContext(ctx), email)
	if findErr == nil && (existing.PasswordHash != "" || existing.Issuer == LocalIssuer) {
		return SignInResult{}, ErrEmailTaken
	}
	if findErr == nil {
		// Same person, already signed in with a provider. The password is
		// stored only after they open the link, so knowing the address is
		// not enough to take the account.
		if s.authMail == nil {
			return SignInResult{}, ErrProviderAccount
		}
		if mailErr := s.sendAuthMail(ctx, existing.ID, existing.Email, "confirm", hash); mailErr != nil {
			return SignInResult{User: existing}, fmt.Errorf("%w: %w", ErrMailFailed, mailErr)
		}
		return SignInResult{User: existing, NeedsConfirm: true}, nil
	}
	if !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return SignInResult{}, findErr
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
		u := store.User{
			Issuer: LocalIssuer, Subject: email, Email: email, Name: name, PasswordHash: hash,
			EmailConfirmed: false, CreatedAt: now, LastLoginAt: now,
		}
		r := q.Raw().Clauses(clause.OnConflict{DoNothing: true}).Create(&u)
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected == 0 {
			return ErrEmailTaken
		}
		// GORM skips a false bool when the column has a default, so set it
		// explicitly. A password account stays unconfirmed until it opens
		// the link. Without system mail it can still sign in.
		if err := q.Raw().Model(&store.User{}).Where("id = ?", u.ID).Update("email_confirmed", false).Error; err != nil {
			return err
		}
		acc, err := q.CreatePersonalAccount(ctx, u.ID, defaultAccountName(u), now)
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
	if err != nil || s.authMail == nil {
		return res, err
	}
	res.NeedsConfirm = true
	if mailErr := s.sendAuthMail(ctx, res.User.ID, res.User.Email, "confirm", ""); mailErr != nil {
		return res, fmt.Errorf("%w: %w", ErrMailFailed, mailErr)
	}
	return res, nil
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
	err = s.db.WithContext(ctx).Where("email = ? AND password_hash <> ''", email).Order("id").Take(&u).Error
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
	if s.authMail != nil && !u.EmailConfirmed {
		return SignInResult{}, ErrEmailUnconfirmed
	}
	if len(s.allowed) > 0 && !s.allowed[email] {
		return SignInResult{}, ErrSignInNotAllowed
	}
	s.db.WithContext(ctx).Model(&store.User{}).Where("id = ?", u.ID).Update("last_login_at", s.now())
	var m store.Membership
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		var err error
		m, err = s.ensurePersonal(ctx, q, u, SourceWeb)
		return err
	})
	if err != nil {
		return SignInResult{}, err
	}
	s.audit(ctx, nil, Actor{UserID: u.ID, AccountID: m.Account.ID, Source: SourceWeb}, "auth.sign_in", email, map[string]any{"method": "password"})
	return SignInResult{User: u, AccountID: m.Account.ID}, nil
}

// busy reports whether a password check failed for lack of capacity rather
// than a wrong password.
func validPassword(pw, email string) error {
	if n := utf8.RuneCountInString(pw); n < minPasswordLen || n > maxPasswordLen {
		return invalid("password", "use %d to %d characters", minPasswordLen, maxPasswordLen)
	}
	if email != "" && strings.EqualFold(pw, email) {
		return invalid("password", "must not be your email address")
	}
	return nil
}

func busy(ctx context.Context, err error) bool {
	return errors.Is(err, password.ErrBusy) || (err != nil && ctx.Err() != nil)
}
