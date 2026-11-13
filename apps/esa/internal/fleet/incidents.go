package fleet

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// IncidentView is one security notice a person can review.
type IncidentView struct {
	ID          int64
	ServerID    int64
	ServerName  string
	Kind        string
	Subject     string
	State       string
	Message     string
	NextStep    string
	FirstSeen   time.Time
	LastSeen    time.Time
	AckedAt     *time.Time
	ResolvedAt  *time.Time
	Recurrences int
	Note        string
	// Until and Reason describe a snooze or an accepted risk.
	Until  *time.Time
	Reason string
	// CanReview is true when the actor may acknowledge, snooze, or accept
	// it: an admin of the space that owns the server.
	CanReview bool
	// Events is the notice's history, oldest first. Only GetIncident fills it.
	Events []IncidentEventView
}

// IncidentEventView is one step in a notice's history.
type IncidentEventView struct {
	Kind   string
	Actor  string
	Detail string
	At     time.Time
}

// Incident event kinds.
const (
	eventOpened       = "opened"
	eventReopened     = "reopened"
	eventRecurred     = "recurred"
	eventSnoozeBroken = "snooze_broken"
	eventResolved     = "resolved"
	eventAcknowledged = "acknowledged"
	eventSnoozed      = "snoozed"
	eventAccepted     = "accepted"
	eventReturned     = "returned"
)

func (s *Service) incidentEvent(ctx context.Context, incidentID int64, kind string, actor *int64, detail string) {
	ev := store.IncidentEvent{IncidentID: incidentID, Kind: kind, ActorID: actor, Detail: trimRunes(detail, 500), At: s.now()}
	if err := s.db.WithContext(context.WithoutCancel(ctx)).Omit("Incident").Create(&ev).Error; err != nil {
		s.log.Error("record incident event", "incident_id", incidentID, "err", err)
	}
}

// openIncident records a notice. Repeating the same server, kind, and
// subject updates the last-seen time.
//
// It opens again, and returns true so the caller may notify, when
// something new happened:
//   - it was resolved and is seen again (a recurrence);
//   - it was reviewed and event is true (the caller saw it start again,
//     such as a second host key change or a socket that came back) or the
//     message changed (the same key now on root);
//   - it was snoozed and event is true: a new occurrence breaks a snooze;
//   - it was accepted as a risk and the message changed.
//
// A condition that merely persists stays where it is.
func (s *Service) openIncident(ctx context.Context, accountID, serverID int64, kind, subject, message string, event bool) (opened bool) {
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
		s.incidentEvent(ctx, row.ID, eventOpened, nil, message)
		return true
	}
	if err != nil {
		s.log.Error("load incident", "err", err)
		return false
	}
	changed := row.Message != message
	reopen := ""
	switch row.State {
	case "resolved":
		reopen = eventRecurred
	case "acknowledged":
		if event || changed {
			reopen = eventReopened
		}
	case "snoozed":
		if event {
			reopen = eventSnoozeBroken
		}
	case "accepted":
		if changed {
			reopen = eventReopened
		}
	}
	changes := map[string]any{"last_seen": now, "message": message}
	if reopen != "" {
		changes["state"], changes["acked_by"], changes["acked_at"] = "open", nil, nil
		changes["snooze_until"], changes["snooze_reason"], changes["resolved_at"] = nil, "", nil
		if reopen == eventRecurred {
			changes["recurrences"] = gorm.Expr("recurrences + 1")
		}
	}
	if err := s.db.WithContext(ctx).Model(&row).Updates(changes).Error; err != nil {
		s.log.Error("update incident", "err", err)
		return false
	}
	if reopen != "" {
		s.incidentEvent(ctx, row.ID, reopen, nil, message)
	}
	return reopen != ""
}

// resolveIncident marks a notice resolved because a fresh check no longer
// saw its condition. Callers must not call it when the data is missing.
func (s *Service) resolveIncident(ctx context.Context, accountID, serverID int64, kind, subject string) {
	now := s.now()
	var row store.SecurityIncident
	err := s.db.WithContext(ctx).Where("account_id = ? AND server_id = ? AND kind = ? AND subject = ? AND state <> ?", accountID, serverID, kind, subject, "resolved").Take(&row).Error
	if err != nil {
		return
	}
	if err := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{
		"state": "resolved", "resolved_at": now, "snooze_until": nil, "snooze_reason": "",
	}).Error; err != nil {
		s.log.Error("resolve incident", "err", err)
		return
	}
	s.incidentEvent(ctx, row.ID, eventResolved, nil, "a fresh check no longer saw it")
}

// returnExpired puts snoozed notices and accepted risks whose time is up
// back on the review list.
func (s *Service) returnExpired(ctx context.Context) {
	var rows []store.SecurityIncident
	if err := s.db.WithContext(ctx).Where("state IN ? AND snooze_until IS NOT NULL AND snooze_until <= ?", []string{"snoozed", "accepted"}, s.now()).Find(&rows).Error; err != nil {
		return
	}
	for _, r := range rows {
		res := s.db.WithContext(ctx).Model(&store.SecurityIncident{}).Where("id = ? AND state = ?", r.ID, r.State).
			Updates(map[string]any{"state": "open", "snooze_until": nil, "snooze_reason": ""})
		if res.Error == nil && res.RowsAffected == 1 {
			s.incidentEvent(ctx, r.ID, eventReturned, nil, "the "+map[string]string{"snoozed": "snooze", "accepted": "accepted risk"}[r.State]+" ended")
		}
	}
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
