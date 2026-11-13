package fleet

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// SendDigests sends the Monday summary for each space that has something
// to say. A control plane that is down on Monday skips that week. An empty
// week sends nothing. The slot is Monday 08:00 UTC.
func (s *Service) SendDigests(ctx context.Context) {
	now := s.now()
	utc := now.UTC()
	if utc.Weekday() != time.Monday || utc.Hour() < 8 {
		return
	}
	var accounts []store.Account
	if err := s.db.WithContext(ctx).Find(&accounts).Error; err != nil {
		s.log.Error("list accounts for digest", "err", err)
		return
	}
	slot := time.Date(utc.Year(), utc.Month(), utc.Day(), 8, 0, 0, 0, time.UTC)
	for _, acc := range accounts {
		if !digestDue(now, acc.DigestAt) {
			continue
		}
		// Claim this week's slot before sending, so a second control plane
		// that read the same account does not send it again.
		res := s.db.WithContext(ctx).Model(&store.Account{}).
			Where("id = ? AND (digest_at IS NULL OR digest_at < ?)", acc.ID, slot).
			Update("digest_at", now)
		if res.Error != nil {
			s.log.Error("record digest", "account_id", acc.ID, "err", res.Error)
			continue
		}
		if res.RowsAffected == 0 {
			continue
		}
		title, body, ok := s.digest(ctx, acc.ID, now)
		if ok {
			s.notify(ctx, acc.ID, Message{Title: title, Body: body, Severity: "info", URL: s.publicURL.JoinPath("/app").String()})
			s.audit(ctx, nil, System(acc.ID), "digest.sent", acc.Name, map[string]any{"title": title})
		}
	}
}

// digestDue reports whether this Monday's summary has not been sent.
// last nil means it has never been sent. The check still requires Monday
// after 08:00 UTC, so a nil last on a Wednesday does not send.
func digestDue(now time.Time, last *time.Time) bool {
	utc := now.UTC()
	if utc.Weekday() != time.Monday || utc.Hour() < 8 {
		return false
	}
	slot := time.Date(utc.Year(), utc.Month(), utc.Day(), 8, 0, 0, 0, time.UTC)
	return last == nil || last.Before(slot)
}

type digestFacts struct {
	Unreachable []string
	Waiting     []string
	Review      []string
}

func digestText(now time.Time, f digestFacts) (title, body string, ok bool) {
	if len(f.Unreachable) == 0 && len(f.Waiting) == 0 && len(f.Review) == 0 {
		return "", "", false
	}
	title = "Week of " + now.UTC().Format("2 Jan")
	var b strings.Builder
	if len(f.Unreachable) > 0 {
		b.WriteString("Unreachable: ")
		b.WriteString(joinCap(f.Unreachable, 10))
		b.WriteString("\n")
	}
	if len(f.Waiting) > 0 {
		b.WriteString("Security updates still waiting: ")
		b.WriteString(joinCap(f.Waiting, 10))
		b.WriteString("\n")
	}
	if len(f.Review) > 0 {
		b.WriteString("Still to review: ")
		b.WriteString(joinCap(f.Review, 10))
		b.WriteString("\n")
	}
	return title, strings.TrimSpace(b.String()), true
}

func joinCap(items []string, n int) string {
	if len(items) > n {
		return strings.Join(items[:n], ", ") + fmt.Sprintf(", and %d more", len(items)-n)
	}
	return strings.Join(items, ", ")
}

func (s *Service) digest(ctx context.Context, accountID int64, now time.Time) (string, string, bool) {
	var facts digestFacts
	type row struct {
		Name         string
		Status       string
		SecuritySeen store.JSON
	}
	var rows []row
	err := s.db.WithContext(ctx).Table("servers").
		Select("servers.name, server_states.status, server_states.security_seen").
		Joins("LEFT JOIN server_states ON server_states.server_id = servers.id").
		Where("servers.account_id = ? AND servers.enrollment = ?", accountID, "ready").
		Order("servers.name").
		Scan(&rows).Error
	if err != nil {
		s.log.Error("digest servers", "account_id", accountID, "err", err)
		return "", "", false
	}
	for _, r := range rows {
		if r.Status == string(StatusUnreachable) {
			facts.Unreachable = append(facts.Unreachable, r.Name)
		}
		var seen map[string]time.Time
		if r.SecuritySeen.Decode(&seen) != nil {
			continue
		}
		for pkg, at := range seen {
			if at.IsZero() || now.Sub(at) < 7*24*time.Hour {
				continue
			}
			days := int(now.Sub(at).Hours() / 24)
			facts.Waiting = append(facts.Waiting, fmt.Sprintf("%s %s for %d days", r.Name, pkg, days))
		}
	}
	var open []store.SecurityIncident
	if err := s.db.WithContext(ctx).Where("account_id = ? AND state = ?", accountID, "open").
		Order("last_seen desc").Limit(20).Find(&open).Error; err != nil {
		s.log.Error("digest incidents", "account_id", accountID, "err", err)
		return "", "", false
	}
	for _, in := range open {
		facts.Review = append(facts.Review, in.Message)
	}
	return digestText(now, facts)
}
