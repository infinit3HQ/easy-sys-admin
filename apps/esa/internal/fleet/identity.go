package fleet

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

	var res SignInResult
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		now := s.now()
		u, err := q.UpsertUser(ctx, id.Issuer, id.Subject, email, strings.TrimSpace(id.Name), now)
		if err != nil {
			return fmt.Errorf("save user: %w", err)
		}
		res.User = u

		m, err := s.ensurePersonal(ctx, q, u, source)
		if err != nil {
			return err
		}
		res.AccountID = m.Account.ID

		s.audit(ctx, q, Actor{UserID: u.ID, AccountID: res.AccountID, Source: source},
			"auth.sign_in", u.Email, map[string]any{"issuer": id.Issuer})
		return nil
	})
	return res, err
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

// ensurePersonal returns the user's membership in their personal space,
// creating the space when they have none (a new user, or one who joined a
// team through an invite before ever having a space of their own).
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
