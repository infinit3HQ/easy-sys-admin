package fleet

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// AlertKind identifies an alert rule.
type AlertKind string

const (
	AlertDisk           AlertKind = "disk"
	AlertInode          AlertKind = "inode"
	AlertMemory         AlertKind = "memory"
	AlertUnreachable    AlertKind = "unreachable"
	AlertFailedUnit     AlertKind = "failed_unit"
	AlertContainer      AlertKind = "container"
	AlertRebootRequired AlertKind = "reboot_required"
	AlertCertExpiry     AlertKind = "cert_expiry"
	AlertRunFailed      AlertKind = "run_failed"
	AlertHostKey        AlertKind = "host_key"
	AlertExposure       AlertKind = "exposure"
	AlertNewPort        AlertKind = "new_port"
	AlertNewKey         AlertKind = "new_key"
	AlertSecurityUpdate AlertKind = "security_update"
)

// defaultRules are created for every account on first use.
var defaultRules = []store.AlertRule{
	{Kind: string(AlertDisk), Threshold: 85},
	{Kind: string(AlertInode), Threshold: 85},
	{Kind: string(AlertMemory), Threshold: 90, ForMinutes: 10},
	{Kind: string(AlertUnreachable), ForMinutes: 3},
	{Kind: string(AlertFailedUnit)},
	{Kind: string(AlertContainer), ForMinutes: 5},
	{Kind: string(AlertRebootRequired), ForMinutes: 7 * 24 * 60},
	{Kind: string(AlertCertExpiry), Threshold: 14},
	{Kind: string(AlertRunFailed)},
	{Kind: string(AlertHostKey)},
	{Kind: string(AlertExposure)},
	{Kind: string(AlertNewPort)},
	{Kind: string(AlertNewKey)},
	{Kind: string(AlertSecurityUpdate), Threshold: 7},
}

func (s *Service) rules(ctx context.Context, accountID int64) (map[AlertKind]store.AlertRule, error) {
	var rows []store.AlertRule
	if err := s.db.WithContext(ctx).Where("account_id = ?", accountID).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) < len(defaultRules) {
		now := s.now()
		for _, d := range defaultRules {
			// Password login and a missing firewall are common. The rule
			// stays off until an admin turns it on.
			enabled := d.Kind != string(AlertExposure)
			d.AccountID, d.Enabled, d.CreatedAt, d.UpdatedAt = accountID, enabled, now, now
			s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Omit(clause.Associations).Create(&d)
		}
		if err := s.db.WithContext(ctx).Where("account_id = ?", accountID).Find(&rows).Error; err != nil {
			return nil, err
		}
	}
	out := make(map[AlertKind]store.AlertRule, len(rows))
	for _, r := range rows {
		out[AlertKind(r.Kind)] = r
	}
	return out, nil
}

// condition is one thing that is currently wrong.
type condition struct {
	kind     AlertKind
	subject  string
	severity string
	message  string
}

// evaluateAlerts compares the server's current conditions with open alerts:
// new conditions become pending, pending ones fire after the rule's
// duration, and alerts whose condition is gone resolve.
func (s *Service) evaluateAlerts(ctx context.Context, srv store.Server, state *store.ServerState) {
	rules, err := s.rules(ctx, srv.AccountID)
	if err != nil {
		s.log.Error("load alert rules", "err", err)
		return
	}
	if state == nil {
		var st store.ServerState
		if s.db.WithContext(ctx).Where("server_id = ?", srv.ID).Take(&st).Error != nil {
			return
		}
		state = &st
	}
	conds := serverConditions(srv, state, rules)
	if !state.HostKeyMismatch && state.Status != string(StatusUnreachable) {
		var seen map[string]time.Time
		if state.SecuritySeen.Decode(&seen) == nil && rules[AlertSecurityUpdate].Enabled {
			if c, ok := securityUpdateCondition(srv.Name, seen, s.now(), rules[AlertSecurityUpdate].Threshold); ok {
				conds = append(conds, c)
			}
		}
	}
	s.reconcile(ctx, srv.AccountID, &srv.ID, rules, conds, func(k AlertKind) bool { return k != AlertCertExpiry && k != AlertRunFailed })
}

func serverConditions(srv store.Server, st *store.ServerState, rules map[AlertKind]store.AlertRule) []condition {
	var out []condition
	add := func(k AlertKind, subject, sev, msg string) {
		if r, ok := rules[k]; ok && r.Enabled {
			out = append(out, condition{kind: k, subject: subject, severity: sev, message: srv.Name + ": " + msg})
		}
	}
	if st.HostKeyMismatch {
		add(AlertHostKey, "", "critical", "host key changed; all work on this server is paused")
		return out
	}
	if st.Status == string(StatusUnreachable) {
		add(AlertUnreachable, "", "critical", "not answering ("+st.LastError+")")
		return out
	}
	if len(st.Snapshot) == 0 {
		return out
	}
	var snap proto.Status
	if st.Snapshot.Decode(&snap) != nil {
		return out
	}
	for _, d := range snap.Disks {
		if p := pct(d.Used, d.Total); p >= rules[AlertDisk].Threshold {
			add(AlertDisk, d.Mount, severityFor(p, 95), fmt.Sprintf("disk %s is %.0f%% full", d.Mount, p))
		}
		if p := pct(d.InodesUsed, d.InodesTotal); p >= rules[AlertInode].Threshold {
			add(AlertInode, d.Mount, severityFor(p, 95), fmt.Sprintf("inodes on %s are %.0f%% used", d.Mount, p))
		}
	}
	if p := pct(snap.MemUsed, snap.MemTotal); p >= rules[AlertMemory].Threshold {
		add(AlertMemory, "", severityFor(p, 97), fmt.Sprintf("memory is %.0f%% used", p))
	}
	for _, u := range snap.FailedUnits {
		add(AlertFailedUnit, u, "warning", "systemd unit "+u+" failed")
	}
	if snap.Docker != nil && snap.Docker.Unhealthy+snap.Docker.Restarting > 0 {
		add(AlertContainer, "", "warning", plural(snap.Docker.Unhealthy+snap.Docker.Restarting, "container is", "containers are")+" unhealthy or restarting")
	}
	if snap.RebootRequired {
		add(AlertRebootRequired, "", "info", "a reboot is required")
	}
	var ex Exposure
	if st.Exposure.Decode(&ex) == nil {
		for _, c := range exposureConditions(ex) {
			add(c.kind, c.subject, c.severity, c.message)
		}
	}
	return out
}

// securityUpdateCondition reports security packages that have been pending
// for at least days. days 0 means any pending security update.
func securityUpdateCondition(server string, seen map[string]time.Time, now time.Time, days float64) (condition, bool) {
	limit := time.Duration(days * float64(24*time.Hour))
	var oldestName string
	var oldest time.Time
	n := 0
	for name, at := range seen {
		if at.IsZero() || now.Sub(at) < limit {
			continue
		}
		n++
		if oldest.IsZero() || at.Before(oldest) {
			oldest, oldestName = at, name
		}
	}
	if n == 0 {
		return condition{}, false
	}
	age := int(now.Sub(oldest).Hours() / 24)
	sev := "warning"
	if age >= 30 {
		sev = "critical"
	}
	msg := fmt.Sprintf("%s: security update %s has been waiting %d days", server, oldestName, age)
	if n > 1 {
		msg = fmt.Sprintf("%s: %d security updates have been waiting, the oldest is %s for %d days", server, n, oldestName, age)
	}
	return condition{kind: AlertSecurityUpdate, subject: "", severity: sev, message: msg}, true
}

func severityFor(p, critical float64) string {
	if p >= critical {
		return "critical"
	}
	return "warning"
}

// reconcile opens, fires, and resolves alerts for one server (or the
// account when serverID is nil). owns says which kinds this caller manages.
func (s *Service) reconcile(ctx context.Context, accountID int64, serverID *int64, rules map[AlertKind]store.AlertRule, conds []condition, owns func(AlertKind) bool) {
	now := s.now()
	q := s.db.WithContext(ctx).Where("account_id = ? AND resolved_at IS NULL", accountID)
	if serverID != nil {
		q = q.Where("server_id = ?", *serverID)
	} else {
		q = q.Where("server_id IS NULL")
	}
	var open []store.Alert
	if err := q.Find(&open).Error; err != nil {
		s.log.Error("load open alerts", "err", err)
		return
	}
	key := func(k AlertKind, subject string) string { return string(k) + "|" + subject }
	current := map[string]condition{}
	for _, c := range conds {
		current[key(c.kind, c.subject)] = c
	}
	seen := map[string]bool{}
	for i := range open {
		al := &open[i]
		k := key(AlertKind(al.Kind), al.Subject)
		if !owns(AlertKind(al.Kind)) {
			continue
		}
		c, still := current[k]
		if !still {
			al.ResolvedAt = &now
			s.db.WithContext(ctx).Model(al).Update("resolved_at", now)
			if al.FiredAt != nil {
				s.notify(ctx, accountID, s.alertMessage(al, "Resolved: "+al.Message, true))
			}
			continue
		}
		seen[k] = true
		updates := map[string]any{"message": c.message, "severity": c.severity}
		if al.FiredAt == nil && now.Sub(al.PendingSince) >= time.Duration(rules[AlertKind(al.Kind)].ForMinutes)*time.Minute {
			updates["fired_at"] = now
			updates["notified"] = true
			al.Message, al.Severity = c.message, c.severity
			s.noteSecurityIncident(ctx, accountID, al)
			s.notify(ctx, accountID, s.alertMessage(al, c.message, false))
		}
		s.db.WithContext(ctx).Model(al).Updates(updates)
	}
	for k, c := range current {
		if seen[k] || !owns(c.kind) {
			continue
		}
		al := store.Alert{
			AccountID: accountID, RuleID: rules[c.kind].ID, ServerID: serverID, Kind: string(c.kind),
			Subject: c.subject, Severity: c.severity, Message: c.message, PendingSince: now,
		}
		if rules[c.kind].ForMinutes == 0 {
			al.FiredAt, al.Notified = &now, true
		}
		if err := s.db.WithContext(ctx).Omit(clause.Associations).Create(&al).Error; err != nil {
			s.log.Error("open alert", "err", err)
			continue
		}
		if al.FiredAt != nil {
			s.noteSecurityIncident(ctx, accountID, &al)
			s.notify(ctx, accountID, s.alertMessage(&al, c.message, false))
		}
	}
}

func (s *Service) alertMessage(al *store.Alert, title string, resolved bool) Message {
	msg := Message{Title: title, Body: al.Message, Severity: al.Severity, Resolved: resolved, Kind: al.Kind, URL: s.alertURL(al)}
	if al.ServerID != nil {
		msg.ServerID = *al.ServerID
	}
	return msg
}

func (s *Service) noteSecurityIncident(ctx context.Context, accountID int64, al *store.Alert) {
	if al.ServerID == nil {
		return
	}
	switch AlertKind(al.Kind) {
	case AlertHostKey, AlertSecurityUpdate, AlertExposure:
		s.openIncident(ctx, accountID, *al.ServerID, al.Kind, al.Subject, al.Message, true)
	}
}

func (s *Service) alertURL(al *store.Alert) string {
	if al.ServerID != nil {
		return s.publicURL.JoinPath("/app/servers", strconv.FormatInt(*al.ServerID, 10)).String()
	}
	return s.publicURL.JoinPath("/app/alerts").String()
}

// alertRunFailed fires a one-off alert for a failed run. It resolves itself
// the next time the same group's run of that kind succeeds.
func (s *Service) alertRunFailed(ctx context.Context, run store.Run) {
	rules, err := s.rules(ctx, run.AccountID)
	if err != nil || !rules[AlertRunFailed].Enabled {
		return
	}
	name := "servers"
	if run.Group != nil {
		name = run.Group.Name
	}
	now := s.now()
	al := store.Alert{
		AccountID: run.AccountID, RuleID: rules[AlertRunFailed].ID, Kind: string(AlertRunFailed),
		Subject: "run:" + strconv.FormatInt(run.ID, 10), Severity: "warning",
		Message:      fmt.Sprintf("%s run on %s %s (%s)", run.Kind, name, run.Status, run.Summary),
		PendingSince: now, FiredAt: &now, Notified: true, ResolvedAt: &now,
	}
	s.db.WithContext(ctx).Omit(clause.Associations).Create(&al)
	s.notify(ctx, run.AccountID, Message{Title: al.Message, Body: al.Message, Severity: "warning",
		Kind: al.Kind, URL: s.publicURL.JoinPath("/app/runs", strconv.FormatInt(run.ID, 10)).String()})
}
