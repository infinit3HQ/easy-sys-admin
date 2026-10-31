package fleet

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// SignIn records a successful login. A user without any account gets a new
// account of their own, with the owner role.
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

		ms, err := q.ListMemberships(ctx, u.ID)
		if err != nil {
			return fmt.Errorf("list memberships: %w", err)
		}
		if len(ms) > 0 {
			res.AccountID = ms[0].Account.ID
		} else {
			acc, err := q.CreateAccount(ctx, defaultAccountName(u), now)
			if err != nil {
				return fmt.Errorf("create account: %w", err)
			}
			if err := q.AddMembership(ctx, acc.ID, u.ID, string(RoleOwner), now); err != nil {
				return fmt.Errorf("add membership: %w", err)
			}
			res.AccountID = acc.ID
			s.audit(ctx, q, Actor{UserID: u.ID, AccountID: acc.ID, Role: RoleOwner, Source: source},
				"account.create", acc.Name, nil)
		}

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
