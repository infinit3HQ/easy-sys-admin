package fleet

import (
	"context"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const (
	maxSnooze     = 7 * 24 * time.Hour
	maxAcceptance = 90 * 24 * time.Hour
	maxNote       = 500
)

// IncidentFilter narrows ListIncidents. The zero value is the review list:
// open notices only.
type IncidentFilter struct {
	// All adds acknowledged, snoozed, accepted, and resolved notices.
	All bool
}

func (s *Service) incidentScope(ctx context.Context, a Actor) *gormDB {
	return s.db.WithContext(ctx).Where("(security_incidents.account_id = ? OR security_incidents.server_id IN ("+sharedServerIDsSQL+"))", a.AccountID, a.AccountID)
}

// ListIncidents returns notices the actor can see, worst first: open ones,
// then by last seen.
func (s *Service) ListIncidents(ctx context.Context, a Actor, f IncidentFilter) ([]IncidentView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	s.returnExpired(ctx)
	q := s.incidentScope(ctx, a).Preload("Server").Order("last_seen DESC").Limit(100)
	if !f.All {
		q = q.Where("state = ?", "open")
	}
	var rows []store.SecurityIncident
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]IncidentView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.incidentView(a, r))
	}
	return out, nil
}

// GetIncident returns one notice with its history.
func (s *Service) GetIncident(ctx context.Context, a Actor, id int64) (IncidentView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return IncidentView{}, err
	}
	s.returnExpired(ctx)
	var row store.SecurityIncident
	if err := s.incidentScope(ctx, a).Preload("Server").Where("security_incidents.id = ?", id).Take(&row).Error; err != nil {
		return IncidentView{}, mapStore(err)
	}
	v := s.incidentView(a, row)
	var events []store.IncidentEvent
	var err error
	if err = s.db.WithContext(ctx).Where("incident_id = ?", id).Order("at, id").Limit(200).Find(&events).Error; err != nil {
		return IncidentView{}, err
	}
	var ids []int64
	for _, e := range events {
		if e.ActorID != nil {
			ids = append(ids, *e.ActorID)
		}
	}
	// People of another organization are not named to a share's viewers.
	names := map[int64]string{}
	if row.AccountID == a.AccountID {
		if names, err = s.userNames(ctx, ids); err != nil {
			return IncidentView{}, err
		}
	}
	v.Events = make([]IncidentEventView, 0, len(events))
	for _, e := range events {
		ev := IncidentEventView{Kind: e.Kind, Detail: e.Detail, At: e.At, Actor: "esa"}
		if e.ActorID != nil {
			ev.Actor = names[*e.ActorID]
			if ev.Actor == "" {
				ev.Actor = "a member of the owning space"
			}
		}
		v.Events = append(v.Events, ev)
	}
	return v, nil
}

func (s *Service) incidentView(a Actor, r store.SecurityIncident) IncidentView {
	return IncidentView{
		ID: r.ID, ServerID: r.ServerID, ServerName: r.Server.Name, Kind: r.Kind, Subject: r.Subject, State: r.State,
		Message: r.Message, NextStep: incidentNextStep(r.Kind), FirstSeen: r.FirstSeen, LastSeen: r.LastSeen,
		AckedAt: r.AckedAt, ResolvedAt: r.ResolvedAt, Recurrences: r.Recurrences, Note: r.Note,
		Until: r.SnoozeUntil, Reason: r.SnoozeReason,
		CanReview: r.AccountID == a.AccountID && a.can(RoleAdmin, ScopeAdmin) == nil,
	}
}

// ownIncident loads a notice of the actor's own space for a triage change.
func (s *Service) ownIncident(ctx context.Context, a Actor, id int64) (store.SecurityIncident, error) {
	var row store.SecurityIncident
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return row, err
	}
	err := s.db.WithContext(ctx).Preload("Server").Where("id = ? AND account_id = ?", id, a.AccountID).Take(&row).Error
	return row, mapStore(err)
}

func cleanIncidentNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if len([]rune(note)) > maxNote {
		return "", invalid("note", "must be at most %d characters", maxNote)
	}
	return note, nil
}

// AcknowledgeIncident records that an admin of the owning space reviewed
// the notice, with an optional note. It does not resolve it and does not
// change the server.
func (s *Service) AcknowledgeIncident(ctx context.Context, a Actor, id int64, note string) (IncidentView, error) {
	note, err := cleanIncidentNote(note)
	if err != nil {
		return IncidentView{}, err
	}
	row, err := s.ownIncident(ctx, a, id)
	if err != nil {
		return IncidentView{}, err
	}
	if row.State == "resolved" {
		return IncidentView{}, invalid("state", "a resolved notice needs no review")
	}
	now := s.now()
	if err := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{
		"state": "acknowledged", "acked_by": a.UserID, "acked_at": now, "note": note,
		"snooze_until": nil, "snooze_reason": "",
	}).Error; err != nil {
		return IncidentView{}, err
	}
	s.incidentEvent(ctx, row.ID, eventAcknowledged, store.OptionalID(a.UserID), note)
	s.audit(ctx, nil, a, "incident.acknowledge", row.Server.Name, map[string]any{"kind": row.Kind, "subject": row.Subject, "note": note})
	return s.GetIncident(ctx, a, id)
}

// SnoozeIncident hides a notice until until, at most 7 days ahead. A new
// occurrence of the same subject breaks the snooze.
func (s *Service) SnoozeIncident(ctx context.Context, a Actor, id int64, until time.Time, reason string) (IncidentView, error) {
	return s.holdIncident(ctx, a, id, until, reason, "snoozed", maxSnooze)
}

// AcceptIncident records an accepted risk with a reason and an expiry, at
// most 90 days ahead. The notice and its evidence stay; it returns to the
// review list at the expiry or when its message changes.
func (s *Service) AcceptIncident(ctx context.Context, a Actor, id int64, until time.Time, reason string) (IncidentView, error) {
	return s.holdIncident(ctx, a, id, until, reason, "accepted", maxAcceptance)
}

func (s *Service) holdIncident(ctx context.Context, a Actor, id int64, until time.Time, reason, state string, limit time.Duration) (IncidentView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 200 {
		return IncidentView{}, invalid("reason", "say why, in at most 200 characters")
	}
	now := s.now()
	if !until.After(now) || until.After(now.Add(limit)) {
		return IncidentView{}, invalid("until", "must be within the next %d days", int(limit/(24*time.Hour)))
	}
	row, err := s.ownIncident(ctx, a, id)
	if err != nil {
		return IncidentView{}, err
	}
	if row.Kind == string(AlertHostKey) {
		return IncidentView{}, invalid("until", "a host key change cannot be snoozed or accepted")
	}
	if row.State == "resolved" {
		return IncidentView{}, invalid("state", "a resolved notice needs no review")
	}
	if err := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{
		"state": state, "snooze_until": until, "snooze_reason": reason,
	}).Error; err != nil {
		return IncidentView{}, err
	}
	kind, action := eventSnoozed, "incident.snooze"
	if state == "accepted" {
		kind, action = eventAccepted, "incident.accept"
	}
	s.incidentEvent(ctx, row.ID, kind, store.OptionalID(a.UserID), reason+" (until "+until.UTC().Format(time.RFC3339)+")")
	s.audit(ctx, nil, a, action, row.Server.Name, map[string]any{"kind": row.Kind, "until": until, "reason": reason})
	return s.GetIncident(ctx, a, id)
}

// userNames returns display names by user id. An empty name falls back to
// the caller's wording; emails are not shown.
func (s *Service) userNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []store.User
	if err := s.db.WithContext(ctx).Select("id", "name").Where("id IN ?", uniqueIDs(ids)).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out, nil
}
