// Package fleet is the service layer. The web app, the JSON API, the CLI,
// and the MCP server all go through it, so permission checks and account
// isolation live in one place.
package fleet

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/crypto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// HelperSource returns the esa-helper binary for a GOARCH.
type HelperSource func(arch string) ([]byte, error)

// Notifier delivers alert messages. internal/notify implements it.
type Notifier interface {
	Send(ctx context.Context, ch store.NotifyChannel, config []byte, msg Message) error
}

// Message is one notification.
type Message struct {
	Title    string
	Body     string
	Severity string // info | warning | critical
	Resolved bool
	URL      string
}

// Deps are the service's collaborators.
type Deps struct {
	Store     *store.Store
	Box       *crypto.Box
	Pool      *remote.Pool
	Helpers   HelperSource
	Notifier  Notifier
	PublicURL *url.URL
	// AllowedEmails limits sign-in to these lowercase addresses. Empty allows everyone.
	AllowedEmails []string
	// AllowPrivateNetworks lets SSH, certificate, and notification targets be
	// private addresses. Loopback, link-local, and metadata are always refused.
	AllowPrivateNetworks bool
	Logger               *slog.Logger
}

// Service implements every fleet operation.
type Service struct {
	store     *store.Store
	db        *gorm.DB
	box       *crypto.Box
	pool      *remote.Pool
	helpers   HelperSource
	notifier  Notifier
	publicURL *url.URL
	allowed   map[string]bool
	// allowPrivate mirrors Deps.AllowPrivateNetworks.
	allowPrivate bool
	log          *slog.Logger
	now          func() time.Time

	// baseCtx outlives requests; background runs use it.
	baseCtx context.Context
	runs    sync.WaitGroup

	signerMu sync.Mutex
	signers  map[int64]*accountSigner

	// serverLocks serialises maintenance runs per server.
	serverLocks sync.Map // int64 -> *sync.Mutex
}

// New returns a Service.
func New(baseCtx context.Context, d Deps) *Service {
	allowed := make(map[string]bool, len(d.AllowedEmails))
	for _, e := range d.AllowedEmails {
		allowed[e] = true
	}
	log := d.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		store: d.Store, db: d.Store.DB(), box: d.Box, pool: d.Pool, helpers: d.Helpers,
		notifier: d.Notifier, publicURL: d.PublicURL, allowed: allowed, allowPrivate: d.AllowPrivateNetworks, log: log,
		now: func() time.Time { return time.Now().UTC() }, baseCtx: baseCtx,
		signers: map[int64]*accountSigner{},
	}
}

// Wait blocks until background runs started by the service have finished.
func (s *Service) Wait() { s.runs.Wait() }

// go runs fn in the background, tracked by Wait.
func (s *Service) background(fn func(ctx context.Context)) {
	s.runs.Add(1)
	go func() {
		defer s.runs.Done()
		fn(s.baseCtx)
	}()
}

// audit writes an audit entry. Failures are logged, not returned.
func (s *Service) audit(ctx context.Context, q *store.Queries, a Actor, action, target string, detail map[string]any) {
	if q == nil {
		q = s.store.Queries
	}
	if a.TokenID != 0 {
		if detail == nil {
			detail = map[string]any{}
		}
		detail["token_id"] = a.TokenID
	}
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

// member checks the actor still belongs to their account.
func (s *Service) member(ctx context.Context, a Actor) error {
	if a.Source == SourceSystem {
		return nil
	}
	if _, err := s.Membership(ctx, a.UserID, a.AccountID); err != nil {
		return err
	}
	return nil
}

// scoped returns a query limited to the actor's account. Every read of an
// account-owned table starts here.
func (s *Service) scoped(ctx context.Context, a Actor) *gorm.DB {
	return s.db.WithContext(ctx).Where("account_id = ?", a.AccountID)
}
