package fleet

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// IncidentView is one security notice a person can mark as reviewed.
type IncidentView struct {
	ID         int64
	ServerID   int64
	ServerName string
	Kind       string
	Subject    string
	State      string
	Message    string
	NextStep   string
	FirstSeen  time.Time
	LastSeen   time.Time
	AckedAt    *time.Time
}

// openIncident records a notice. Repeating the same server, kind, and
// subject updates the last-seen time. It does not send a second notice and
// does not undo an acknowledgement or a snooze. created is true only the
// first time.
func (s *Service) openIncident(ctx context.Context, accountID, serverID int64, kind, subject, message string) (created bool) {
	if len(subject) > 200 {
		subject = subject[:200]
	}
	now := s.now()
	var row store.SecurityIncident
	err := s.db.WithContext(ctx).Where("account_id = ? AND server_id = ? AND kind = ? AND subject = ?", accountID, serverID, kind, subject).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row = store.SecurityIncident{
			AccountID: accountID, ServerID: serverID, Kind: kind, Subject: subject,
			State: "open", Message: message, FirstSeen: now, LastSeen: now,
		}
		if err := s.db.WithContext(ctx).Omit("Account", "Server").Create(&row).Error; err != nil {
			s.log.Error("open incident", "err", err)
			return false
		}
		return true
	}
	if err != nil {
		s.log.Error("load incident", "err", err)
		return false
	}
	if err := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{"last_seen": now, "message": message}).Error; err != nil {
		s.log.Error("update incident", "err", err)
	}
	return false
}

// ListIncidents returns notices the actor can see. includeAcknowledged adds
// ones a person has already reviewed.
func (s *Service) ListIncidents(ctx context.Context, a Actor, includeAcknowledged bool) ([]IncidentView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	s.db.WithContext(ctx).Model(&store.SecurityIncident{}).
		Where("state = ? AND snooze_until IS NOT NULL AND snooze_until <= ?", "snoozed", s.now()).
		Updates(map[string]any{"state": "open", "snooze_until": nil, "snooze_reason": ""})
	q := s.db.WithContext(ctx).Where("(security_incidents.account_id = ? OR security_incidents.server_id IN ("+sharedServerIDsSQL+"))", a.AccountID, a.AccountID).
		Preload("Server").Order("last_seen DESC").Limit(100)
	if !includeAcknowledged {
		q = q.Where("state = ?", "open")
	}
	var rows []store.SecurityIncident
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]IncidentView, 0, len(rows))
	for _, r := range rows {
		v := IncidentView{ID: r.ID, ServerID: r.ServerID, Kind: r.Kind, Subject: r.Subject, State: r.State,
			Message: r.Message, FirstSeen: r.FirstSeen, LastSeen: r.LastSeen, AckedAt: r.AckedAt, ServerName: r.Server.Name}
		v.NextStep = incidentNextStep(r.Kind)
		out = append(out, v)
	}
	return out, nil
}

// AcknowledgeIncident records that an admin of the owning space has reviewed
// the notice. It does not change the server.
func (s *Service) AcknowledgeIncident(ctx context.Context, a Actor, id int64) (IncidentView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return IncidentView{}, err
	}
	var row store.SecurityIncident
	if err := s.db.WithContext(ctx).Preload("Server").Where("id = ? AND account_id = ?", id, a.AccountID).Take(&row).Error; err != nil {
		return IncidentView{}, mapStore(err)
	}
	now := s.now()
	if err := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{
		"state": "acknowledged", "acked_by": a.UserID, "acked_at": now,
	}).Error; err != nil {
		return IncidentView{}, err
	}
	s.audit(ctx, nil, a, "incident.acknowledge", row.Server.Name, map[string]any{"kind": row.Kind, "subject": row.Subject})
	row.State, row.AckedAt = "acknowledged", &now
	return IncidentView{ID: row.ID, ServerID: row.ServerID, ServerName: row.Server.Name, Kind: row.Kind, Subject: row.Subject,
		State: row.State, Message: row.Message, NextStep: incidentNextStep(row.Kind), FirstSeen: row.FirstSeen, LastSeen: row.LastSeen, AckedAt: row.AckedAt}, nil
}

// SnoozeIncident hides a notice until until, which must be within 7 days.
// Seeing the same subject again leaves the snooze in place. A different
// subject is a separate notice.
func (s *Service) SnoozeIncident(ctx context.Context, a Actor, id int64, until time.Time, reason string) (IncidentView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return IncidentView{}, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 200 {
		return IncidentView{}, invalid("reason", "say why, in at most 200 characters")
	}
	now := s.now()
	if !until.After(now) || until.After(now.Add(7*24*time.Hour)) {
		return IncidentView{}, invalid("until", "must be within the next 7 days")
	}
	var row store.SecurityIncident
	if err := s.db.WithContext(ctx).Preload("Server").Where("id = ? AND account_id = ?", id, a.AccountID).Take(&row).Error; err != nil {
		return IncidentView{}, mapStore(err)
	}
	if row.Kind == string(AlertHostKey) {
		return IncidentView{}, invalid("until", "a host key change cannot be snoozed")
	}
	if err := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{
		"state": "snoozed", "snooze_until": until, "snooze_reason": reason,
	}).Error; err != nil {
		return IncidentView{}, err
	}
	s.audit(ctx, nil, a, "incident.snooze", row.Server.Name, map[string]any{"kind": row.Kind, "until": until, "reason": reason})
	return IncidentView{ID: row.ID, ServerID: row.ServerID, ServerName: row.Server.Name, Kind: row.Kind, Subject: row.Subject,
		State: "snoozed", Message: row.Message, NextStep: incidentNextStep(row.Kind), FirstSeen: row.FirstSeen, LastSeen: row.LastSeen}, nil
}

// incidentNextStep is a hint for a person. It does not change the server.
func incidentNextStep(kind string) string {
	switch kind {
	case string(AlertHostKey):
		return "Compare this key with the server console. Accept it only when it matches, or remove the server."
	case string(AlertNewKey):
		return "Check who added the key. Approving the recorded keys does not remove access."
	case string(AlertNewPort):
		return "Check which process listens there. This is not a reachability result."
	case string(AlertExposure):
		return "Read the exposure report. A socket on all interfaces is not proof the port is reachable."
	case string(AlertSecurityUpdate):
		return "Read the package list, then start an update only after a person approves the plan."
	default:
		return "Read the notice. Mark it reviewed when you have seen it."
	}
}
