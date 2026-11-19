package fleet

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

var probeSourcePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,40}$`)

const probeFreshFor = 15 * time.Minute

type ProbeInput struct {
	Source, Host, State, Reason string
	Port                        int
	ObservedAt                  time.Time
}
type ProbeView struct {
	Source, Host, State, Reason, LastKnownState string
	Port                                        int
	AttemptedAt                                 time.Time
	LastKnownAt                                 *time.Time
	Stale                                       bool
}

func (s *Service) GetReachability(ctx context.Context, a Actor, id int64) ([]ProbeView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	srv, _, err := s.loadVisibleServer(ctx, a, id)
	if err != nil {
		return nil, err
	}
	var rows []store.ProbeObservation
	if err := s.db.WithContext(ctx).Where("account_id = ? AND server_id = ?", srv.AccountID, srv.ID).Order("source, port").Limit(128).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ProbeView, 0, len(rows))
	for _, r := range rows {
		stale := s.now().Sub(r.AttemptedAt) > probeFreshFor || !strings.EqualFold(r.Host, srv.Host)
		state := r.State
		if stale {
			state = "unknown"
		}
		out = append(out, ProbeView{Source: r.Source, Host: r.Host, Port: r.Port, State: state, Reason: r.Reason, AttemptedAt: r.AttemptedAt, LastKnownState: r.LastKnownState, LastKnownAt: r.LastKnownAt, Stale: stale})
	}
	return out, nil
}

// SubmitReachability records evidence. It never makes a network connection.
func (s *Service) SubmitReachability(ctx context.Context, a Actor, id int64, in ProbeInput) error {
	if !a.hasScope(ScopeActions) {
		return ErrForbidden
	}
	srv, err := s.loadServer(ctx, a, id)
	if err != nil {
		return err
	}
	role, err := s.roleOn(ctx, a, srv.GroupID)
	if err != nil {
		return err
	}
	if err := canAct(a, role, RoleAdmin, ScopeActions); err != nil {
		return err
	}
	if srv.Connection == "outbound" {
		return invalid("host", "outbound servers have no enrolled SSH address to probe")
	}
	if !probeSourcePattern.MatchString(in.Source) {
		return invalid("source", "use 1 to 40 letters, digits, dots, underscores, or hyphens")
	}
	if !strings.EqualFold(in.Host, srv.Host) {
		return invalid("host", "must equal the server's enrolled host")
	}
	if in.Port < 1 || in.Port > 65535 {
		return invalid("port", "must be between 1 and 65535")
	}
	valid := (in.State == "reachable" && in.Reason == "none") || (in.State == "not_reached" && (in.Reason == "refused" || in.Reason == "timeout")) || (in.State == "unknown" && (in.Reason == "dns_failed" || in.Reason == "network_error"))
	if !valid {
		return invalid("state", "use reachable with none, not_reached with refused or timeout, or unknown with dns_failed or network_error")
	}
	now := s.now()
	if in.ObservedAt.IsZero() || in.ObservedAt.After(now.Add(time.Minute)) || in.ObservedAt.Before(now.Add(-24*time.Hour)) {
		return invalid("observedAt", "must be within the last 24 hours, allowing at most 1 minute of clock skew")
	}
	return s.store.InTx(ctx, func(q *store.Queries) error {
		db := q.Raw().WithContext(ctx)
		// Serialize source creation and enforce the evidence cap without eviction.
		var locked store.Server
		if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND account_id = ?", id, a.AccountID).Take(&locked).Error; err != nil {
			return mapStore(err)
		}
		if !strings.EqualFold(in.Host, locked.Host) {
			return invalid("host", "the server address changed; run the probe again")
		}
		var row store.ProbeObservation
		err := db.Where("account_id = ? AND server_id = ? AND source = ? AND port = ?", a.AccountID, id, in.Source, in.Port).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var count int64
			if err := db.Model(&store.ProbeObservation{}).Where("server_id = ?", id).Count(&count).Error; err != nil {
				return err
			}
			if count >= 128 {
				return &ConflictError{Message: "this server already has 128 probe sources and ports"}
			}
			row = store.ProbeObservation{AccountID: a.AccountID, ServerID: id, Source: in.Source, Port: in.Port}
		} else if err != nil {
			return err
		} else if !in.ObservedAt.After(row.AttemptedAt) {
			return nil
		}
		if !strings.EqualFold(row.Host, in.Host) {
			row.LastKnownAt = nil
			row.LastKnownState = ""
		}
		row.Host, row.State, row.Reason, row.AttemptedAt = in.Host, in.State, in.Reason, in.ObservedAt
		if in.State != "unknown" {
			row.LastKnownState, row.LastKnownAt = in.State, &in.ObservedAt
		}
		if err := db.Omit("Account", "Server").Save(&row).Error; err != nil {
			return err
		}
		s.audit(ctx, q, a, "probe.observe", srv.Name, map[string]any{"source": in.Source, "port": in.Port, "state": in.State, "observed_at": in.ObservedAt})
		return nil
	})
}
