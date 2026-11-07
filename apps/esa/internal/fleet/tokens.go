package fleet

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// tokenPrefix marks esa API tokens, so secret scanners can find leaked ones.
const tokenPrefix = "esa_"

// Token lifetimes. Every new token expires.
const (
	DefaultTokenTTL = 90 * 24 * time.Hour
	MaxTokenTTL     = 365 * 24 * time.Hour
)

// TokenView is an API token without its secret.
type TokenView struct {
	ID         int64
	Name       string
	Scopes     []Scope
	Prefix     string
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

func toTokenView(t store.APIToken) TokenView {
	v := TokenView{ID: t.ID, Name: t.Name, Prefix: t.Prefix, LastUsedAt: t.LastUsedAt, ExpiresAt: t.ExpiresAt, RevokedAt: t.RevokedAt, CreatedAt: t.CreatedAt, Scopes: []Scope{}}
	_ = t.Scopes.Decode(&v.Scopes)
	return v
}

// ListTokens returns the caller's own tokens in the current account.
func (s *Service) ListTokens(ctx context.Context, a Actor) ([]TokenView, error) {
	if a.TokenID != 0 {
		return nil, ErrForbidden // tokens cannot manage tokens
	}
	var rows []store.APIToken
	if err := s.scoped(ctx, a).Where("user_id = ?", a.UserID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]TokenView, 0, len(rows))
	for _, r := range rows {
		out = append(out, toTokenView(r))
	}
	return out, nil
}

// CreateToken issues a token. The secret is returned once and never stored.
// Scopes beyond the user's role are refused: admin needs an org admin, and
// logs:read and actions need an org admin or a viewer in a team. A zero ttl
// means DefaultTokenTTL; otherwise it must be one day to MaxTokenTTL.
func (s *Service) CreateToken(ctx context.Context, a Actor, name string, scopes []Scope, ttl time.Duration) (TokenView, string, error) {
	if a.TokenID != 0 || a.Source == SourceSystem {
		return TokenView{}, "", ErrForbidden
	}
	name, err := validName("name", name)
	if err != nil {
		return TokenView{}, "", err
	}
	if ttl == 0 {
		ttl = DefaultTokenTTL
	}
	if ttl < 24*time.Hour || ttl > MaxTokenTTL {
		return TokenView{}, "", invalid("expiresInDays", "use 1 to 365 days")
	}
	if len(scopes) == 0 {
		scopes = []Scope{ScopeRead}
	}
	var clean []Scope
	inTeam, teamChecked := false, false
	for _, sc := range scopes {
		if !slices.Contains(AllScopes, sc) {
			return TokenView{}, "", invalid("scopes", "unknown scope %q", sc)
		}
		// Org admins may grant any scope. A viewer may grant logs:read and
		// actions only when a team of the organization makes them admin of
		// some groups; each use is still checked on the group.
		ok := a.Role.AtLeast(RoleAdmin)
		switch {
		case ok:
		case sc == ScopeRead:
			ok = a.Role.AtLeast(RoleViewer)
		case (sc == ScopeLogs || sc == ScopeActions) && a.Role == RoleViewer:
			if !teamChecked {
				if inTeam, err = s.inAnyTeam(ctx, a); err != nil {
					return TokenView{}, "", err
				}
				teamChecked = true
			}
			ok = inTeam
		}
		if !ok {
			return TokenView{}, "", invalid("scopes", "your role cannot grant %q", sc)
		}
		if !slices.Contains(clean, sc) {
			clean = append(clean, sc)
		}
	}
	if !slices.Contains(clean, ScopeRead) {
		clean = append([]Scope{ScopeRead}, clean...)
	}
	secret, hash := randomToken(tokenPrefix)
	now := s.now()
	exp := now.Add(ttl)
	t := store.APIToken{
		UserID: a.UserID, AccountID: a.AccountID, Name: name, Scopes: store.MustJSON(clean),
		TokenHash: hash, Prefix: secret[:len(tokenPrefix)+6], CreatedAt: now, ExpiresAt: &exp,
	}
	if err := s.db.WithContext(ctx).Omit(clause.Associations).Create(&t).Error; err != nil {
		return TokenView{}, "", mapStore(err)
	}
	s.audit(ctx, nil, a, "token.create", name, map[string]any{"scopes": clean, "id": t.ID})
	return toTokenView(t), secret, nil
}

// RevokeToken revokes one of the caller's tokens.
func (s *Service) RevokeToken(ctx context.Context, a Actor, id int64) error {
	if a.TokenID != 0 {
		return ErrForbidden
	}
	res := s.scoped(ctx, a).Model(&store.APIToken{}).Where("id = ? AND user_id = ? AND revoked_at IS NULL", id, a.UserID).Update("revoked_at", s.now())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	s.audit(ctx, nil, a, "token.revoke", strconv.FormatInt(id, 10), nil)
	return nil
}

// AuthenticateToken turns a bearer token into an actor. The actor's role is
// the user's current role, so removing someone from an account also
// disables their tokens for it.
func (s *Service) AuthenticateToken(ctx context.Context, secret string, source Source) (Actor, bool, error) {
	if !strings.HasPrefix(secret, tokenPrefix) || len(secret) > 200 {
		return Actor{}, false, nil
	}
	var t store.APIToken
	if err := s.db.WithContext(ctx).Where("token_hash = ?", hashToken(secret)).Take(&t).Error; err != nil {
		if mapStore(err) == ErrNotFound {
			return Actor{}, false, nil
		}
		return Actor{}, false, err
	}
	now := s.now()
	if t.RevokedAt != nil || (t.ExpiresAt != nil && !now.Before(*t.ExpiresAt)) {
		return Actor{}, false, nil
	}
	m, err := s.Membership(ctx, t.UserID, t.AccountID)
	if err != nil {
		return Actor{}, false, nil
	}
	if ok, err := s.UserAllowed(ctx, t.UserID); err != nil || !ok {
		return Actor{}, false, err
	}
	if ok, err := s.LoginAllowed(ctx, t.UserID); err != nil || !ok {
		return Actor{}, false, err
	}
	if t.LastUsedAt == nil || now.Sub(*t.LastUsedAt) > time.Minute {
		s.db.WithContext(ctx).Model(&store.APIToken{}).Where("id = ?", t.ID).Update("last_used_at", now)
	}
	var scopes []Scope
	_ = t.Scopes.Decode(&scopes)
	if scopes == nil {
		scopes = []Scope{}
	}
	return Actor{UserID: t.UserID, AccountID: t.AccountID, Role: Role(m.Role), Source: source, Scopes: scopes, TokenID: t.ID}, true, nil
}
