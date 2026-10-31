// Package auth signs users in (OIDC or local dev mode) and turns session
// cookies into fleet.Actor values for the rest of the server.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const (
	// sessionTTL is the absolute limit; idleTTL ends sessions unused for a day.
	sessionTTL      = 7 * 24 * time.Hour
	idleTTL         = 24 * time.Hour
	touchInterval   = 5 * time.Minute
	sessionTokenLen = 32
)

// Sessions issues and checks session cookies. The cookie holds a random
// token; the database holds only its SHA-256.
type Sessions struct {
	store  *store.Store
	fleet  *fleet.Service
	secure bool
	log    *slog.Logger
	now    func() time.Time
}

// NewSessions returns a session manager. secure sets the Secure flag and the
// __Host- cookie prefix, and must be true whenever the site is served over HTTPS.
func NewSessions(st *store.Store, fl *fleet.Service, secure bool, log *slog.Logger) *Sessions {
	return &Sessions{store: st, fleet: fl, secure: secure, log: log, now: time.Now}
}

// CookieName is the session cookie name for this install.
func (m *Sessions) CookieName() string {
	if m.secure {
		return "__Host-esa_session"
	}
	return "esa_session"
}

// Start creates a session for the user in the given account and sets the
// cookie. A session the request already carried is deleted, so a token
// planted before sign-in is never upgraded.
func (m *Sessions) Start(ctx context.Context, w http.ResponseWriter, r *http.Request, userID, accountID int64) error {
	if old, ok := m.tokenHash(r); ok {
		if err := m.store.DeleteSession(ctx, old); err != nil {
			return err
		}
	}
	token := make([]byte, sessionTokenLen)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	now := m.now()
	err := m.store.CreateSession(ctx, store.Session{
		TokenHash:  hashToken(token),
		UserID:     userID,
		AccountID:  accountID,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(sessionTTL),
	})
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     m.CookieName(),
		Value:    base64.RawURLEncoding.EncodeToString(token),
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// End deletes the request's session, if any, and clears the cookie.
func (m *Sessions) End(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	defer m.clearCookie(w)
	hash, ok := m.tokenHash(r)
	if !ok {
		return nil
	}
	return m.store.DeleteSession(ctx, hash)
}

// EndAll deletes every session of the request's user and clears the cookie.
// Requests without a session (including API tokens) only clear the cookie.
func (m *Sessions) EndAll(ctx context.Context, w http.ResponseWriter) error {
	defer m.clearCookie(w)
	actor, ok := fleet.ActorFrom(ctx)
	if _, hasSession := ctx.Value(sessionHashKey{}).([]byte); !ok || !hasSession {
		return nil
	}
	return m.store.DB().WithContext(ctx).Where("user_id = ?", actor.UserID).Delete(&store.Session{}).Error
}

// SwitchAccount moves the current request's session to another account the user belongs to.
func (m *Sessions) SwitchAccount(ctx context.Context, accountID int64) (store.Membership, error) {
	actor, ok := fleet.ActorFrom(ctx)
	hash, hasToken := ctx.Value(sessionHashKey{}).([]byte)
	if !ok || !hasToken {
		return store.Membership{}, fleet.ErrForbidden
	}
	to, err := m.fleet.Membership(ctx, actor.UserID, accountID)
	if err != nil {
		return store.Membership{}, err
	}
	if err := m.store.SetSessionAccount(ctx, hash, accountID); err != nil {
		return store.Membership{}, err
	}
	m.fleet.RecordAccountSwitch(ctx, actor, to)
	return to, nil
}

// Middleware resolves the session cookie and, when it is valid, stores a
// fleet.Actor in the request context. Requests without a valid session pass
// through without an actor; handlers decide whether that is allowed.
func (m *Sessions) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := fleet.ActorFrom(r.Context()); ok {
			next.ServeHTTP(w, r) // already authenticated with a bearer token
			return
		}
		actor, hash, ok, err := m.resolve(r)
		if err != nil {
			m.log.Error("resolve session", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if ok {
			ctx := fleet.WithActor(r.Context(), actor)
			ctx = context.WithValue(ctx, sessionHashKey{}, hash)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

type sessionHashKey struct{}

func (m *Sessions) resolve(r *http.Request) (actor fleet.Actor, hash []byte, ok bool, err error) {
	actor, ok, err = m.resolveActor(r)
	if ok {
		hash, _ = m.tokenHash(r)
	}
	return actor, hash, ok, err
}

func (m *Sessions) resolveActor(r *http.Request) (fleet.Actor, bool, error) {
	ctx := r.Context()
	hash, ok := m.tokenHash(r)
	if !ok {
		return fleet.Actor{}, false, nil
	}
	sess, err := m.store.GetSession(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		return fleet.Actor{}, false, nil
	}
	if err != nil {
		return fleet.Actor{}, false, err
	}
	now := m.now()
	if !now.Before(sess.ExpiresAt) || now.Sub(sess.LastSeenAt) >= idleTTL {
		return fleet.Actor{}, false, m.store.DeleteSession(ctx, hash)
	}
	// Removing someone from the allow list ends their sessions.
	if ok, err := m.fleet.UserAllowed(ctx, sess.UserID); err != nil {
		return fleet.Actor{}, false, err
	} else if !ok {
		return fleet.Actor{}, false, m.store.DeleteSession(ctx, hash)
	}

	membership, err := m.fleet.Membership(ctx, sess.UserID, sess.AccountID)
	if errors.Is(err, fleet.ErrForbidden) {
		// The user was removed from the session's account. Fall back to
		// another account they still belong to, or end the session.
		membership, err = m.fleet.FirstMembership(ctx, sess.UserID)
		if errors.Is(err, fleet.ErrForbidden) {
			return fleet.Actor{}, false, m.store.DeleteSession(ctx, hash)
		}
		if err != nil {
			return fleet.Actor{}, false, err
		}
		if err := m.store.SetSessionAccount(ctx, hash, membership.Account.ID); err != nil {
			return fleet.Actor{}, false, err
		}
	} else if err != nil {
		return fleet.Actor{}, false, err
	}

	if now.Sub(sess.LastSeenAt) > touchInterval {
		if err := m.store.TouchSession(ctx, hash, now); err != nil {
			m.log.Warn("touch session", "err", err)
		}
	}

	return fleet.Actor{
		UserID:    sess.UserID,
		AccountID: membership.Account.ID,
		Role:      fleet.Role(membership.Role),
		Source:    fleet.SourceWeb,
	}, true, nil
}

// CleanupExpired deletes expired and idle sessions. Run it periodically.
func (m *Sessions) CleanupExpired(ctx context.Context) {
	now := m.now()
	n, err := m.store.DeleteExpiredSessions(ctx, now)
	if err != nil {
		m.log.Error("delete expired sessions", "err", err)
		return
	}
	idle := m.store.DB().WithContext(ctx).Where("last_seen_at <= ?", now.Add(-idleTTL)).Delete(&store.Session{})
	if idle.Error != nil {
		m.log.Error("delete idle sessions", "err", idle.Error)
		return
	}
	n += idle.RowsAffected
	if n > 0 {
		m.log.Info("deleted expired sessions", "count", n)
	}
}

func (m *Sessions) tokenHash(r *http.Request) ([]byte, bool) {
	c, err := r.Cookie(m.CookieName())
	if err != nil {
		return nil, false
	}
	token, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || len(token) != sessionTokenLen {
		return nil, false
	}
	return hashToken(token), true
}

func (m *Sessions) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.CookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func hashToken(token []byte) []byte {
	sum := sha256.Sum256(token)
	return sum[:]
}
