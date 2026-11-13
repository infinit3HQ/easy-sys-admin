package fleet

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// Warning delivery modes.
const (
	DeliverImmediate = "immediate"
	DeliverDaily     = "daily"
)

// summaryListed caps how many warnings a daily summary names.
const summaryListed = 20

// Routing is how a space's warnings reach its channels.
type Routing struct {
	Warnings string
	Hour     int
	Timezone string
}

// alwaysImmediate are notices that never wait for a summary: an access
// change must reach someone now.
func alwaysImmediate(msg Message) bool {
	if msg.Summary || msg.Severity == "critical" {
		return true
	}
	return msg.Kind == string(AlertHostKey) || msg.Kind == string(AlertNewKey)
}

// holdForSummary stores a warning for the daily summary when the space
// asked for one. It returns false when the message should be sent now.
func (s *Service) holdForSummary(ctx context.Context, accountID int64, msg Message) bool {
	if alwaysImmediate(msg) {
		return false
	}
	var acc store.Account
	if err := s.db.WithContext(ctx).Select("id", "warning_delivery").Where("id = ?", accountID).Take(&acc).Error; err != nil {
		return false
	}
	if acc.WarningDelivery != DeliverDaily {
		return false
	}
	row := store.HeldWarning{AccountID: accountID, Title: trimRunes(msg.Title, 300), URL: msg.URL, CreatedAt: s.now()}
	if err := s.db.WithContext(context.WithoutCancel(ctx)).Omit("Account").Create(&row).Error; err != nil {
		s.log.Error("hold warning", "account_id", accountID, "err", err)
		return false
	}
	return true
}

// GetRouting returns the space's warning delivery setting.
func (s *Service) GetRouting(ctx context.Context, a Actor) (Routing, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return Routing{}, err
	}
	var acc store.Account
	if err := s.db.WithContext(ctx).Where("id = ?", a.AccountID).Take(&acc).Error; err != nil {
		return Routing{}, mapStore(err)
	}
	return Routing{Warnings: acc.WarningDelivery, Hour: acc.WarningHour, Timezone: acc.WarningTimezone}, nil
}

// SetRouting changes how warnings are delivered. Switching back to
// immediate sends what was held at once.
func (s *Service) SetRouting(ctx context.Context, a Actor, r Routing) (Routing, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return Routing{}, err
	}
	if r.Warnings != DeliverImmediate && r.Warnings != DeliverDaily {
		return Routing{}, invalid("warnings", "must be immediate or daily")
	}
	if r.Hour < 0 || r.Hour > 23 {
		return Routing{}, invalid("hour", "must be from 0 to 23")
	}
	if r.Timezone == "" {
		r.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(r.Timezone); err != nil {
		return Routing{}, invalid("timezone", "unknown time zone %q", r.Timezone)
	}
	if err := s.db.WithContext(ctx).Model(&store.Account{}).Where("id = ?", a.AccountID).Updates(map[string]any{
		"warning_delivery": r.Warnings, "warning_hour": r.Hour, "warning_timezone": r.Timezone,
	}).Error; err != nil {
		return Routing{}, err
	}
	s.audit(ctx, nil, a, "routing.update", "", map[string]any{"warnings": r.Warnings, "hour": r.Hour, "timezone": r.Timezone})
	if r.Warnings == DeliverImmediate {
		s.sendHeld(ctx, a.AccountID)
	}
	return r, nil
}

// SendWarningSummaries sends each daily space its held warnings once a
// day at its chosen hour. The day's slot is claimed in the database
// first, so a second control plane or a restart does not send it twice.
func (s *Service) SendWarningSummaries(ctx context.Context) {
	var accounts []store.Account
	if err := s.db.WithContext(ctx).Where("warning_delivery = ?", DeliverDaily).Find(&accounts).Error; err != nil {
		s.log.Error("list daily warning spaces", "err", err)
		return
	}
	now := s.now()
	for _, acc := range accounts {
		slot, ok := summarySlot(now, acc.WarningHour, acc.WarningTimezone)
		if !ok || (acc.WarningDigestAt != nil && !acc.WarningDigestAt.Before(slot)) {
			continue
		}
		res := s.db.WithContext(ctx).Model(&store.Account{}).
			Where("id = ? AND (warning_digest_at IS NULL OR warning_digest_at < ?)", acc.ID, slot).
			Update("warning_digest_at", now)
		if res.Error != nil || res.RowsAffected == 0 {
			continue
		}
		s.sendHeld(ctx, acc.ID)
	}
}

// summarySlot is the most recent hour:00 in tz at or before now.
func summarySlot(now time.Time, hour int, tz string) (time.Time, bool) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, false
	}
	local := now.In(loc)
	slot := time.Date(local.Year(), local.Month(), local.Day(), hour, 0, 0, 0, loc)
	if slot.After(local) {
		slot = slot.AddDate(0, 0, -1)
	}
	return slot, true
}

// sendHeld sends one summary of the space's held warnings and removes them.
func (s *Service) sendHeld(ctx context.Context, accountID int64) {
	var held []store.HeldWarning
	if err := s.db.WithContext(ctx).Where("account_id = ?", accountID).Order("id").Find(&held).Error; err != nil || len(held) == 0 {
		return
	}
	title, body := summaryText(held)
	s.notify(ctx, accountID, Message{Title: title, Body: body, Severity: "warning", URL: s.publicURL.JoinPath("/app/alerts").String(), Summary: true})
	ids := make([]int64, len(held))
	for i, h := range held {
		ids[i] = h.ID
	}
	if err := s.db.WithContext(context.WithoutCancel(ctx)).Where("id IN ?", ids).Delete(&store.HeldWarning{}).Error; err != nil {
		s.log.Error("clear held warnings", "account_id", accountID, "err", err)
	}
}

func summaryText(held []store.HeldWarning) (string, string) {
	title := fmt.Sprintf("%d warnings since %s", len(held), held[0].CreatedAt.UTC().Format("Jan 2 15:04 MST"))
	if len(held) == 1 {
		title = "1 warning since " + held[0].CreatedAt.UTC().Format("Jan 2 15:04 MST")
	}
	var b strings.Builder
	for i, h := range held {
		if i == summaryListed {
			fmt.Fprintf(&b, "and %d more on the alerts page\n", len(held)-summaryListed)
			break
		}
		b.WriteString("- " + h.Title + "\n")
	}
	return title, strings.TrimRight(b.String(), "\n")
}
