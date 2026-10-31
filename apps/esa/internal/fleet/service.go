// Package fleet is the service layer. The web app, the JSON API, and the MCP
// server all go through it, so permission checks and account isolation live
// in one place.
package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

var (
	// ErrForbidden means the actor may not do this, or the target is in another account.
	ErrForbidden = errors.New("fleet: forbidden")
	// ErrSignInNotAllowed means the identity is not on the allowed list.
	ErrSignInNotAllowed = errors.New("fleet: sign-in not allowed for this user")
)

// Options configures the service.
type Options struct {
	// AllowedEmails limits sign-in to these lowercase addresses. Empty allows everyone.
	AllowedEmails []string
	Logger        *slog.Logger
}

// Service implements every fleet operation.
type Service struct {
	store   *store.Store
	allowed map[string]bool
	log     *slog.Logger
	now     func() time.Time
}

// New returns a Service backed by st.
func New(st *store.Store, opts Options) *Service {
	allowed := make(map[string]bool, len(opts.AllowedEmails))
	for _, e := range opts.AllowedEmails {
		allowed[e] = true
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: st, allowed: allowed, log: log, now: time.Now}
}

// audit writes an audit entry. Failures are logged, not returned, so a full
// disk does not turn every read into an error.
func (s *Service) audit(ctx context.Context, q *store.Queries, a Actor, action, target string, detail map[string]any) {
	var body []byte
	if len(detail) > 0 {
		if b, err := json.Marshal(detail); err == nil {
			body = b
		}
	}
	err := q.InsertAudit(ctx, store.AuditEntry{
		AccountID: store.OptionalID(a.AccountID),
		UserID:    store.OptionalID(a.UserID),
		Action:    action,
		Target:    target,
		Detail:    string(body),
		Source:    string(a.Source),
	})
	if err != nil {
		s.log.Error("write audit log", "action", action, "err", err)
	}
}
