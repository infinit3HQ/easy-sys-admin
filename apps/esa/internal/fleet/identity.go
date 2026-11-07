package fleet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// Identity is what the identity provider says about a user.
type Identity struct {
	Issuer  string
	Subject string
	Email   string
	Name    string
}

// SignInResult is the user and the account their new session starts in.
type SignInResult struct {
	User      store.User
	AccountID int64
	// NeedsConfirm is set for a password account that cannot sign in until
	// it opens the confirmation link. Provider sign-in never sets it.
	NeedsConfirm bool
}

// SignIn records a successful login. A user without a personal space gets
// one, with the owner role. The new session starts in the personal space.
func (s *Service) SignIn(ctx context.Context, id Identity, source Source) (SignInResult, error) {
	if id.Issuer == "" || id.Subject == "" {
		return SignInResult{}, errors.New("fleet: identity needs issuer and subject")
	}
	email := strings.ToLower(strings.TrimSpace(id.Email))
	if len(s.allowed) > 0 && !s.allowed[email] {
		return SignInResult{}, ErrSignInNotAllowed
	}

	name := strings.TrimSpace(id.Name)
	var res SignInResult
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		now := s.now()
		db := q.Raw().WithContext(ctx)
		u, linked, err := s.upsertIdentity(db, id.Issuer, id.Subject, email, name, now)
		if err != nil {
			return fmt.Errorf("save user: %w", err)
		}
		res.User = u

		m, err := s.ensurePersonal(ctx, q, u, source)
		if err != nil {
			return err
		}
		res.AccountID = m.Account.ID

		detail := map[string]any{"issuer": id.Issuer}
		if linked {
			detail["linked"] = true
		}
		s.audit(ctx, q, Actor{UserID: u.ID, AccountID: res.AccountID, Source: source},
			"auth.sign_in", u.Email, detail)
		return nil
	})
	return res, err
}

// upsertIdentity finds the person behind a provider sign-in. A verified
// email that already belongs to someone joins that account. An empty email
// never matches another user, because the provider did not prove it.
func (s *Service) upsertIdentity(db *gorm.DB, issuer, subject, email, name string, now time.Time) (store.User, bool, error) {
	var ident store.UserIdentity
	err := db.Where("issuer = ? AND subject = ?", issuer, subject).Take(&ident).Error
	var u store.User
	linked := false
	switch {
	case err == nil:
		if err := db.Where("id = ?", ident.UserID).Take(&u).Error; err != nil {
			return store.User{}, false, err
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		if email != "" {
			found, ferr := userByEmail(db, email)
			if ferr == nil {
				u = found
				linked = true
			} else if !errors.Is(ferr, gorm.ErrRecordNotFound) {
				return store.User{}, false, ferr
			}
		}
		if u.ID == 0 {
			u = store.User{
				Issuer: issuer, Subject: subject, Email: email, Name: name,
				EmailConfirmed: email != "", CreatedAt: now, LastLoginAt: now,
			}
			if err := db.Create(&u).Error; err != nil {
				return store.User{}, false, err
			}
		}
		row := store.UserIdentity{UserID: u.ID, Issuer: issuer, Subject: subject, CreatedAt: now}
		if err := db.Create(&row).Error; err != nil {
			return store.User{}, false, err
		}
	default:
		return store.User{}, false, err
	}

	updates := map[string]any{"last_login_at": now}
	if name != "" {
		updates["name"] = name
	}
	if email != "" {
		updates["email"] = email
		updates["email_confirmed"] = true
	}
	if err := db.Model(&store.User{}).Where("id = ?", u.ID).Updates(updates).Error; err != nil {
		return store.User{}, false, err
	}
	if err := db.Where("id = ?", u.ID).Take(&u).Error; err != nil {
		return store.User{}, false, err
	}
	return u, linked, nil
}

// userByEmail returns the person who already uses this address. A password
// account wins over a provider account when both exist.
func userByEmail(db *gorm.DB, email string) (store.User, error) {
	var users []store.User
	if err := db.Where("email = ?", email).Order("id").Find(&users).Error; err != nil {
		return store.User{}, err
	}
	if len(users) == 0 {
		return store.User{}, gorm.ErrRecordNotFound
	}
	for _, u := range users {
		if u.Issuer == LocalIssuer || u.PasswordHash != "" {
			return u, nil
		}
	}
	return users[0], nil
}

// UserAllowed reports whether the user's stored email is still on the allow
// list. Sessions and tokens check it on every request, so removing someone
// from ESA_ALLOWED_EMAILS locks them out without waiting for expiry.
func (s *Service) UserAllowed(ctx context.Context, userID int64) (bool, error) {
	if len(s.allowed) == 0 {
		return true, nil
	}
	u, err := s.store.GetUser(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return s.allowed[strings.ToLower(u.Email)], nil
}

// LoginAllowed reports whether this user may hold a session or a token.
// A password account must open its confirmation link when system mail is
// on. Google and other providers already check the address, so they pass.
// With system mail off, password accounts pass too.
func (s *Service) LoginAllowed(ctx context.Context, userID int64) (bool, error) {
	if s.authMail == nil {
		return true, nil
	}
	u, err := s.store.GetUser(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if u.Issuer != LocalIssuer {
		return true, nil
	}
	return u.EmailConfirmed, nil
}

// ensurePersonal returns the user's membership in their personal space,
// creating the space when they have none.
func (s *Service) ensurePersonal(ctx context.Context, q *store.Queries, u store.User, source Source) (store.Membership, error) {
	// Lock the user row so two first sign-ins at once cannot both create a
	// space (accounts.personal_of is unique). NO KEY UPDATE serialises
	// callers like FOR UPDATE does, without blocking the foreign-key checks
	// of concurrent inserts that reference the user.
	if err := q.Raw().WithContext(ctx).Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).
		Where("id = ?", u.ID).Take(&store.User{}).Error; err != nil {
		return store.Membership{}, fmt.Errorf("lock user: %w", mapStore(err))
	}
	ms, err := q.ListMemberships(ctx, u.ID)
	if err != nil {
		return store.Membership{}, fmt.Errorf("list memberships: %w", err)
	}
	if m, ok := personalMembership(ms, u.ID); ok {
		return m, nil
	}
	now := s.now()
	acc, err := q.CreatePersonalAccount(ctx, u.ID, defaultAccountName(u), now)
	if err != nil {
		return store.Membership{}, fmt.Errorf("create personal space: %w", err)
	}
	if err := q.AddMembership(ctx, acc.ID, u.ID, string(RoleOwner), now); err != nil {
		return store.Membership{}, fmt.Errorf("add membership: %w", err)
	}
	s.audit(ctx, q, Actor{UserID: u.ID, AccountID: acc.ID, Role: RoleOwner, Source: source},
		"account.create", acc.Name, map[string]any{"personal": true})
	return store.Membership{AccountID: acc.ID, UserID: u.ID, Role: string(RoleOwner), CreatedAt: now, Account: acc, User: u}, nil
}

func defaultAccountName(u store.User) string {
	name := u.Name
	if name == "" {
		name, _, _ = strings.Cut(u.Email, "@")
	}
	if name == "" {
		return "My fleet"
	}
	return name + "'s fleet"
}
