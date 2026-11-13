package api

import (
	"encoding/json"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// Conversions from fleet views to generated API types. The fleet package
// never depends on the wire format; this file is the only bridge.

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func optional[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

func nonNil[T any](xs []T) []T {
	if xs == nil {
		return []T{}
	}
	return xs
}

func mapSlice[A, B any](xs []A, f func(A) B) []B {
	out := make([]B, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

func toMembership(m store.Membership) AccountMembership {
	return AccountMembership{Id: m.Account.ID, Name: m.Account.Name, Role: Role(m.Role), Personal: m.Account.Personal()}
}

func toMe(me fleet.Me, a fleet.Actor) Me {
	out := Me{
		User:     User{Id: me.User.ID, Email: me.User.Email, Name: me.User.Name},
		Account:  toMembership(me.Current),
		Accounts: mapSlice(me.Accounts, toMembership),
		ViaToken: a.TokenID != 0,
		Limits:   AccountLimits{MaxServers: me.MaxServers, MaxOrganizations: me.MaxOrganizations},
	}
	if a.TokenID != 0 {
		scopes := make([]Scope, 0, len(a.Scopes))
		for _, s := range a.Scopes {
			scopes = append(scopes, Scope(s))
		}
		out.Scopes = &scopes
	}
	return out
}

func toServer(v fleet.ServerView) Server {
	s := Server{
		Id: v.ID, Name: v.Name, Host: v.Host, Port: v.Port, Username: v.Username,
		GroupId: v.GroupID, GroupName: optional(v.GroupName), Enrollment: ServerEnrollment(v.Enrollment),
		HostKeyFingerprint: v.HostKeyFingerprint, HelperVersion: v.HelperVersion, Arch: v.Arch,
		HelperMissing: ptrTo(nonNil(v.HelperMissing)), HelperCheckedAt: v.HelperCheckedAt,
		HostsControlPlane: v.HostsControlPlane, ProtectedContainers: nonNil(v.ProtectedContainers),
		HasStoredLogin: v.HasStoredLogin, Note: v.Note, CreatedAt: v.CreatedAt, Status: ServerStatus(v.Status),
		Reasons: nonNil(v.Reasons), LastSeenAt: v.LastSeenAt, LastError: v.LastError,
		HostKeyMismatch: v.HostKeyMismatch, PresentedFingerprint: v.PresentedFingerprint,
		Access: groupAccess(v.Access), OwnerTeam: optional(v.OwnerTeam), YourRole: EffectiveRole(v.YourRole),
	}
	if snap := v.Snapshot; snap != nil {
		m := ServerMetrics{
			CpuPercent: snap.CPUPercent, Load1: snap.Load1, UptimeSeconds: snap.UptimeSeconds,
			RebootRequired: snap.RebootRequired, FailedUnits: len(snap.FailedUnits), Os: snap.OS, Kernel: snap.Kernel,
		}
		if snap.MemTotal > 0 {
			m.MemPercent = round1(100 * float64(snap.MemUsed) / float64(snap.MemTotal))
		}
		for _, d := range snap.Disks {
			if d.Total > 0 {
				if p := round1(100 * float64(d.Used) / float64(d.Total)); p > m.DiskPercent {
					m.DiskPercent = p
				}
			}
		}
		if snap.Docker != nil {
			m.ContainersRunning = snap.Docker.Running
			m.ContainersUnhealthy = snap.Docker.Unhealthy + snap.Docker.Restarting
		}
		s.Metrics = &m
	}
	if v.Updates != nil {
		s.Updates = &UpdateCounts{Security: v.Updates.Security, Other: v.Updates.Other, CheckedAt: v.UpdatesAt}
	}
	if ex := v.Exposure; ex != nil {
		s.Exposure = &Exposure{
			CheckedAt: ex.CheckedAt, AttemptedAt: ex.AttemptedAt, Ready: ex.Ready, Stale: ex.Stale(time.Now()),
			PublicPorts: nonNil(ex.PublicPorts), ListeningOnAllInterfaces: nonNil(ex.ListeningOnAllInterfaces),
			SpecificBinds: nonNil(ex.SpecificBinds),
			PasswordAuth:  ex.PasswordAuth, PermitRootLogin: ex.PermitRootLogin,
			PermitEmptyPasswords: ex.PermitEmptyPasswords, PubkeyAuth: ex.PubkeyAuth,
			SshRead: ex.SSHRead, Firewall: ex.Firewall, ExtraRoot: ex.ExtraRoot, NoPasswordAll: ex.NoPasswordAll,
			Uncovered: ex.Uncovered, Gaps: nonNil(ex.Gaps),
		}
	}
	return s
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

func toServerDetail(v fleet.ServerView) ServerDetail {
	return ServerDetail{Server: toServer(v), Snapshot: v.Snapshot, PendingUpdates: v.Updates}
}

func toOverview(ov fleet.Overview) FleetOverview {
	t := ov.Totals
	return FleetOverview{
		Totals: FleetTotals{
			Servers: t.Servers, Groups: t.Groups, Ok: t.OK, Warning: t.Warning,
			Failing: t.Failing, Unreachable: t.Unreachable, Pending: t.Pending,
		},
		Servers: mapSlice(ov.Servers, toServer),
	}
}

func toEnrollment(e fleet.Enrollment) Enrollment {
	return Enrollment{Server: toServer(e.Server), Command: e.Command, ScriptUrl: e.ScriptURL, ExpiresAt: e.ExpiresAt}
}

func toDays(ds []string) []Day {
	out := make([]Day, 0, len(ds))
	for _, d := range ds {
		out = append(out, Day(d))
	}
	return out
}

func toGroup(g fleet.GroupView) Group {
	return Group{
		Id: g.ID, Name: g.Name, Timezone: g.Timezone, ServerCount: g.ServerCount, CreatedAt: g.CreatedAt,
		Access: groupAccess(g.Access), OwnerTeam: optional(g.OwnerTeam), ShareId: g.ShareID, YourRole: EffectiveRole(g.YourRole),
		NextClean: g.NextClean, NextUpdate: g.NextUpdate, LastRuns: mapSlice(g.LastRuns, toRun),
		Clean: CleanPolicy{
			Enabled: g.Clean.Enabled, Days: toDays(g.Clean.Days), Time: g.Clean.Time, BuildCache: g.Clean.BuildCache,
			Vscode: g.Clean.VSCode, Journal: g.Clean.Journal, JournalSize: g.Clean.JournalSize,
		},
		Updates: UpdatePolicy{
			Enabled: g.Updates.Enabled, Mode: UpdatePolicyMode(g.Updates.Mode), Days: toDays(g.Updates.Days),
			Time: g.Updates.Time, Hold: nonNil(g.Updates.Hold), RebootIfRequired: g.Updates.RebootIfRequired,
			BatchSize: g.Updates.BatchSize, MaxFailures: g.Updates.MaxFailures, PlanRequired: g.Updates.PlanRequired,
		},
		Checks: toAPIChecks(g.Checks),
	}
}

func toAPIChecks(in []fleet.HealthCheck) []HealthCheck {
	out := make([]HealthCheck, 0, len(in))
	for _, c := range in {
		item := HealthCheck{Name: c.Name, Kind: HealthCheckKind(c.Kind), Blocking: c.Blocking}
		if c.Port != 0 {
			p := c.Port
			item.Port = &p
		}
		if c.TLS {
			v := true
			item.Tls = &v
		}
		if c.Method != "" {
			m := HealthCheckMethod(c.Method)
			item.Method = &m
		}
		if c.Path != "" {
			p := c.Path
			item.Path = &p
		}
		if c.Unit != "" {
			u := c.Unit
			item.Unit = &u
		}
		if c.Container != "" {
			n := c.Container
			item.Container = &n
		}
		out = append(out, item)
	}
	return out
}

func toRun(r fleet.RunView) Run {
	out := Run{
		Id: r.ID, Kind: RunKind(r.Kind), Trigger: RunTrigger(r.Trigger), Status: RunStatus(r.Status),
		GroupId: r.GroupID, GroupName: optional(r.GroupName), Options: r.Options, StartedAt: r.StartedAt,
		FinishedAt: r.FinishedAt, Summary: r.Summary, BytesFreed: r.BytesFreed,
		Servers: mapSlice(r.Servers, func(s fleet.RunServerView) RunServer {
			return RunServer{
				ServerId: s.ServerID, ServerName: s.ServerName, Status: RunServerStatus(s.Status), Error: s.Error,
				BytesFreed: s.BytesFreed, Detail: jsonObject(s.Detail), StartedAt: s.StartedAt, FinishedAt: s.FinishedAt,
			}
		}),
	}
	if r.Gate != nil {
		out.Gate = &RunGate{State: RunGateState(r.Gate.State), At: r.Gate.At, By: r.Gate.By, Note: r.Gate.Note}
	}
	return out
}

func jsonObject(j store.JSON) map[string]any {
	out := map[string]any{}
	if len(j) > 0 {
		_ = json.Unmarshal(j, &out)
	}
	return out
}

func toAlert(a fleet.AlertView) Alert {
	return Alert{
		Id: a.ID, Kind: a.Kind, ServerId: a.ServerID, ServerName: optional(a.ServerName), Subject: a.Subject,
		Severity: AlertSeverity(a.Severity), Message: a.Message, PendingSince: a.PendingSince, FiredAt: a.FiredAt, ResolvedAt: a.ResolvedAt,
	}
}

func toIncident(v fleet.IncidentView) Incident {
	out := Incident{
		Id: v.ID, ServerId: v.ServerID, ServerName: v.ServerName, Kind: v.Kind, Subject: v.Subject,
		State: IncidentState(v.State), Message: v.Message, NextStep: v.NextStep, FirstSeen: v.FirstSeen, LastSeen: v.LastSeen, AckedAt: v.AckedAt, CanReview: v.CanReview,
		ResolvedAt: v.ResolvedAt, Recurrences: ptrTo(v.Recurrences), Note: ptrTo(v.Note), SnoozeUntil: v.Until, SnoozeReason: ptrTo(v.Reason),
	}
	if v.Events != nil {
		events := make([]IncidentEvent, 0, len(v.Events))
		for _, e := range v.Events {
			events = append(events, IncidentEvent{Kind: IncidentEventKind(e.Kind), Actor: e.Actor, Detail: e.Detail, At: e.At})
		}
		out.Events = &events
	}
	return out
}

func toAlertRule(r fleet.AlertRuleView) AlertRule {
	return AlertRule{Id: r.ID, Kind: AlertRuleKind(r.Kind), Enabled: r.Enabled, Threshold: r.Threshold, ForMinutes: r.ForMinutes}
}

func toChannel(c fleet.ChannelView) Channel {
	return Channel{
		Id: c.ID, Name: c.Name, Kind: ChannelKind(c.Kind), Enabled: c.Enabled, Target: c.Target,
		LastError: c.LastError, LastSentAt: c.LastSentAt, CreatedAt: c.CreatedAt,
	}
}

func toCert(c fleet.CertView) Cert {
	return Cert{
		Id: c.ID, Hostname: c.Hostname, Port: c.Port, ExpiresAt: c.ExpiresAt, DaysLeft: c.DaysLeft,
		Issuer: c.Issuer, LastError: c.LastError, CheckedAt: c.CheckedAt,
	}
}

func toToken(t fleet.TokenView) Token {
	scopes := make([]Scope, 0, len(t.Scopes))
	for _, s := range t.Scopes {
		scopes = append(scopes, Scope(s))
	}
	return Token{
		Id: t.ID, Name: t.Name, Scopes: scopes, Prefix: t.Prefix, LastUsedAt: t.LastUsedAt,
		ExpiresAt: t.ExpiresAt, RevokedAt: t.RevokedAt, CreatedAt: t.CreatedAt,
	}
}

func toActivity(e fleet.ActivityEntry) ActivityEntry {
	return ActivityEntry{
		Id: e.ID, Action: e.Action, Target: e.Target, Source: ActivityEntrySource(e.Source),
		UserName: e.UserName, Detail: jsonObject(e.Detail), CreatedAt: e.CreatedAt,
	}
}

func ptrTo[T any](v T) *T { return &v }
