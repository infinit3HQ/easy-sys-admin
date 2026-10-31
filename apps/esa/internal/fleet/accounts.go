package fleet

import (
	"context"
	"errors"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// Me describes the caller: who they are, the account they are working in,
// and every account they can switch to.
type Me struct {
	User     store.User
	Current  store.Membership
	Accounts []store.Membership
}

// Me returns the caller's user, current account, and memberships.
func (s *Service) Me(ctx context.Context, a Actor) (Me, error) {
	u, err := s.store.GetUser(ctx, a.UserID)
	if err != nil {
		return Me{}, err
	}
	ms, err := s.store.ListMemberships(ctx, a.UserID)
	if err != nil {
		return Me{}, err
	}
	for _, m := range ms {
		if m.Account.ID == a.AccountID {
			return Me{User: u, Current: m, Accounts: ms}, nil
		}
	}
	return Me{}, ErrForbidden
}

// Membership returns the user's role in an account. It returns ErrForbidden
// when the user is not a member, so callers cannot tell missing accounts
// from other people's accounts.
func (s *Service) Membership(ctx context.Context, userID, accountID int64) (store.Membership, error) {
	m, err := s.store.GetMembership(ctx, userID, accountID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Membership{}, ErrForbidden
	}
	return m, err
}

// FirstMembership returns the account a session should default to: the
// user's personal space, or their oldest membership when they have no
// personal space. It returns ErrForbidden if they have no membership.
func (s *Service) FirstMembership(ctx context.Context, userID int64) (store.Membership, error) {
	ms, err := s.store.ListMemberships(ctx, userID)
	if err != nil {
		return store.Membership{}, err
	}
	return defaultMembership(ms, userID)
}

// defaultMembership picks the personal space from ms, else the first
// (oldest) entry.
func defaultMembership(ms []store.Membership, userID int64) (store.Membership, error) {
	if m, ok := personalMembership(ms, userID); ok {
		return m, nil
	}
	if len(ms) == 0 {
		return store.Membership{}, ErrForbidden
	}
	return ms[0], nil
}

// personalMembership returns the user's membership in their own personal space.
func personalMembership(ms []store.Membership, userID int64) (store.Membership, bool) {
	for _, m := range ms {
		if p := m.Account.PersonalOf; p != nil && *p == userID {
			return m, true
		}
	}
	return store.Membership{}, false
}

// RecordAccountSwitch writes the audit entry for a session moving to another account.
// The caller must already have checked membership with Membership.
func (s *Service) RecordAccountSwitch(ctx context.Context, a Actor, to store.Membership) {
	s.audit(ctx, s.store.Queries, Actor{UserID: a.UserID, AccountID: to.Account.ID, Role: Role(to.Role), Source: a.Source},
		"session.switch_account", to.Account.Name, map[string]any{"from_account_id": a.AccountID})
}
