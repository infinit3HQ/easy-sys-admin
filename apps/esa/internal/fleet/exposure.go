package fleet

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const (
	exposureEvery = time.Hour
	// exposurePerCycle caps how many servers collect reports in one poll
	// cycle. The access report can read a day's SSH journal, so doing it
	// for the whole fleet at once would starve status polls.
	exposurePerCycle = 8
)

// Exposure is the small summary stored for the board. It has no user
// list, no key material, and no login addresses.
type Exposure struct {
	CheckedAt   time.Time `json:"checkedAt"`
	Ready       bool      `json:"ready"`
	PublicPorts []string  `json:"publicPorts"`
	// ListeningOnAllInterfaces is the same list as PublicPorts. The old name
	// stays so existing clients keep working. Neither list is a reachability check.
	ListeningOnAllInterfaces []string  `json:"listeningOnAllInterfaces,omitempty"`
	AttemptedAt              time.Time `json:"attemptedAt,omitempty"`
	PasswordAuth             bool      `json:"passwordAuth"`
	PermitRootLogin          string    `json:"permitRootLogin"`
	PermitEmptyPasswords     bool      `json:"permitEmptyPasswords"`
	PubkeyAuth               bool      `json:"pubkeyAuth"`
	SSHRead                  bool      `json:"sshRead"`
	Firewall                 string    `json:"firewall"`
	ExtraRoot                int       `json:"extraRoot"`
	NoPasswordAll            int       `json:"noPasswordAll"`
	// Uncovered is how many sockets on every interface ufw does not mention.
	Uncovered int `json:"uncovered"`
	// Gaps names checks that did not run. An empty listener list is not clean
	// when "listening sockets" is included.
	Gaps []string `json:"gaps,omitempty"`
	// Baseline* is the previous public ports and key fingerprints. The API
	// does not copy these. The first ready report seeds them and does not alert.
	BaselineSeeded bool     `json:"baselineSeeded,omitempty"`
	BaselinePorts  []string `json:"baselinePorts,omitempty"`
	BaselineKeys   []string `json:"baselineKeys,omitempty"`
}

func summarizeExposure(access proto.AccessReport, sec proto.SecurityReport, at time.Time) Exposure {
	ex := Exposure{
		CheckedAt: at, Ready: access.Revision >= proto.ReportRevision && sec.Revision >= proto.ReportRevision,
		PublicPorts: []string{},
	}
	if !ex.Ready {
		return ex
	}
	seen := map[string]bool{}
	for _, l := range sec.Listening {
		if l.Exposure != "all" {
			continue
		}
		p := l.Proto
		switch {
		case strings.HasPrefix(p, "tcp"):
			p = "tcp"
		case strings.HasPrefix(p, "udp"):
			p = "udp"
		}
		key := strconv.Itoa(l.Port) + "/" + p
		if seen[key] {
			continue
		}
		seen[key] = true
		ex.PublicPorts = append(ex.PublicPorts, key)
	}
	slices.Sort(ex.PublicPorts)
	ex.ListeningOnAllInterfaces = ex.PublicPorts
	ex.Gaps = mergeGaps(access.Gaps, sec.Gaps)
	ex.PasswordAuth = access.PasswordAuth
	ex.PermitRootLogin = access.PermitRootLogin
	ex.PermitEmptyPasswords = access.PermitEmptyPasswords
	ex.PubkeyAuth = access.PubkeyAuth
	ex.SSHRead = access.SSHRead
	ex.Firewall = sec.Firewall
	if sec.Firewall == "ufw active" {
		for _, l := range sec.Listening {
			if l.Exposure == "all" && l.UFW == "unmentioned" {
				ex.Uncovered++
			}
		}
	}
	for _, u := range access.Users {
		if u.UID == 0 && u.Name != "root" {
			ex.ExtraRoot++
		}
	}
	for _, r := range access.SudoRules {
		if r.Unrestricted && r.NoPassword {
			ex.NoPasswordAll++
		}
	}
	return ex
}

func exposureChanged(prev, next Exposure) bool {
	if !prev.Ready || !next.Ready {
		return false
	}
	return prev.PasswordAuth != next.PasswordAuth ||
		prev.PermitRootLogin != next.PermitRootLogin ||
		prev.PermitEmptyPasswords != next.PermitEmptyPasswords ||
		prev.PubkeyAuth != next.PubkeyAuth ||
		prev.SSHRead != next.SSHRead ||
		prev.Firewall != next.Firewall ||
		prev.ExtraRoot != next.ExtraRoot ||
		prev.NoPasswordAll != next.NoPasswordAll ||
		!slices.Equal(prev.PublicPorts, next.PublicPorts)
}

func exposureConditions(ex Exposure) []condition {
	if !ex.Ready {
		return nil
	}
	var out []condition
	add := func(subject, sev, msg string) {
		out = append(out, condition{kind: AlertExposure, subject: subject, severity: sev, message: msg})
	}
	if ex.PasswordAuth {
		add("password", "warning", "SSH password login is allowed")
	}
	if ex.PermitEmptyPasswords {
		add("empty-password", "critical", "SSH allows empty passwords")
	}
	if ex.PermitRootLogin == "yes" {
		add("root-login", "warning", "SSH allows root to log in with a password")
	}
	if ex.Firewall == "none" {
		add("firewall", "warning", "no firewall is active")
	}
	if ex.ExtraRoot > 0 {
		add("extra-root", "critical", "a login account other than root has uid 0")
	}
	if ex.NoPasswordAll > 0 {
		add("nopasswd", "warning", "a sudo rule grants NOPASSWD ALL")
	}
	return out
}

// maybeExposure collects the summary when it is due. budgeted is true
// during a fleet poll, which only starts a few of these per cycle.
func (s *Service) maybeExposure(ctx context.Context, srv store.Server, state *store.ServerState, budgeted bool) {
	if state.ExposureAt != nil && s.now().Sub(*state.ExposureAt) < exposureEvery {
		return
	}
	if budgeted && !s.takeExposureSlot() {
		return
	}
	var prev Exposure
	_ = state.Exposure.Decode(&prev)
	ex, keys, ok := s.collectExposure(ctx, srv)
	now := s.now()
	if !ok {
		// A cancelled poll cycle should try again next minute. Any other
		// failure waits an hour so a slow server cannot crowd out status polls.
		if ctx.Err() == nil {
			s.db.WithContext(ctx).Model(&store.ServerState{}).Where("server_id = ?", srv.ID).
				Update("exposure_at", now)
			state.ExposureAt = &now
		}
		return
	}
	if exposureChanged(prev, ex) {
		s.audit(ctx, nil, System(srv.AccountID), "server.exposure", srv.Name, map[string]any{
			"publicPorts": ex.PublicPorts, "passwordAuth": ex.PasswordAuth,
			"permitRootLogin": ex.PermitRootLogin, "permitEmptyPasswords": ex.PermitEmptyPasswords,
			"firewall": ex.Firewall, "extraRoot": ex.ExtraRoot, "noPasswordAll": ex.NoPasswordAll,
		})
	}
	if ex.Ready {
		listenOK := !hasGap(ex.Gaps, "listening sockets")
		ports := ex.PublicPorts
		if !listenOK && prev.BaselineSeeded {
			ex.PublicPorts = prev.PublicPorts
			ex.ListeningOnAllInterfaces = prev.ListeningOnAllInterfaces
			ports = prev.BaselinePorts
		}
		if prev.Ready && prev.BaselineSeeded {
			s.noteAdditions(ctx, srv, prev.BaselinePorts, ports, prev.BaselineKeys, keys)
		}
		ex.BaselineSeeded = true
		if listenOK {
			ex.BaselinePorts = ex.PublicPorts
		} else {
			ex.BaselinePorts = prev.BaselinePorts
		}
		ex.BaselineKeys = keyIDs(keys)
	}
	raw := store.MustJSON(ex)
	s.db.WithContext(ctx).Model(&store.ServerState{}).Where("server_id = ?", srv.ID).
		Updates(map[string]any{"exposure": raw, "exposure_at": now})
	state.Exposure, state.ExposureAt = raw, &now
}

func (s *Service) takeExposureSlot() bool {
	for {
		n := s.exposureBudget.Load()
		if n <= 0 {
			return false
		}
		if s.exposureBudget.CompareAndSwap(n, n-1) {
			return true
		}
	}
}

func (s *Service) collectExposure(ctx context.Context, srv store.Server) (Exposure, []keyUse, bool) {
	ep, err := s.endpoint(ctx, srv)
	if err != nil {
		s.log.Warn("exposure report", "server_id", srv.ID, "err", err)
		return Exposure{}, nil, false
	}
	var access proto.AccessReport
	cctx, cancel := context.WithTimeout(s.netCtx(ctx), 45*time.Second)
	err = s.pool.Call(cctx, ep, &access, "access")
	cancel()
	if err != nil {
		s.noteExposureErr(ctx, srv, err)
		return Exposure{}, nil, false
	}
	var sec proto.SecurityReport
	cctx, cancel = context.WithTimeout(s.netCtx(ctx), 30*time.Second)
	err = s.pool.Call(cctx, ep, &sec, "security")
	cancel()
	if err != nil {
		s.noteExposureErr(ctx, srv, err)
		return Exposure{}, nil, false
	}
	return summarizeExposure(access, sec, s.now()), keyUses(access), true
}

// keyUse is one authorized-key fingerprint and the login that holds it.
type keyUse struct {
	Fingerprint string
	User        string
}

func hasGap(gaps []string, name string) bool {
	return slices.Contains(gaps, name)
}

func mergeGaps(parts ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, part := range parts {
		for _, g := range part {
			if g == "" || seen[g] {
				continue
			}
			seen[g] = true
			out = append(out, g)
		}
	}
	slices.Sort(out)
	return out
}

func keyUses(access proto.AccessReport) []keyUse {
	var out []keyUse
	seen := map[string]bool{}
	for _, u := range access.Users {
		for _, k := range u.AuthorizedKeys {
			if k.Fingerprint == "" || seen[k.Fingerprint] {
				continue
			}
			seen[k.Fingerprint] = true
			out = append(out, keyUse{Fingerprint: k.Fingerprint, User: u.Name})
		}
	}
	slices.SortFunc(out, func(a, b keyUse) int { return strings.Compare(a.Fingerprint, b.Fingerprint) })
	return out
}

func keyIDs(keys []keyUse) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.Fingerprint
	}
	return out
}

func added(prev, cur []string) []string {
	have := map[string]bool{}
	for _, p := range prev {
		have[p] = true
	}
	var out []string
	for _, c := range cur {
		if !have[c] {
			out = append(out, c)
		}
	}
	return out
}

// noteAdditions sends one-shot alerts for public ports and SSH keys that
// were not in the previous report. The alerts are stored already resolved
// so the next hour does not send a "resolved" message.
func (s *Service) noteAdditions(ctx context.Context, srv store.Server, prevPorts, ports, prevKeys []string, keys []keyUse) {
	rules, err := s.rules(ctx, srv.AccountID)
	if err != nil {
		return
	}
	if rules[AlertNewPort].Enabled {
		s.oneShot(ctx, srv, rules[AlertNewPort], AlertNewPort, added(prevPorts, ports), "new socket on all interfaces")
	}
	if rules[AlertNewKey].Enabled {
		var fresh []string
		have := map[string]bool{}
		for _, p := range prevKeys {
			have[p] = true
		}
		for _, k := range keys {
			if !have[k.Fingerprint] {
				fresh = append(fresh, k.Fingerprint+" on "+k.User)
			}
		}
		s.oneShot(ctx, srv, rules[AlertNewKey], AlertNewKey, fresh, "new SSH key")
	}
}

func (s *Service) oneShot(ctx context.Context, srv store.Server, rule store.AlertRule, kind AlertKind, items []string, noun string) {
	if len(items) == 0 {
		return
	}
	send := items
	extra := 0
	if len(send) > 8 {
		extra = len(send) - 8
		send = send[:8]
	}
	now := s.now()
	fire := func(subject, msg string) {
		if len(subject) > 200 {
			subject = subject[:200]
		}
		al := store.Alert{
			AccountID: srv.AccountID, RuleID: rule.ID, ServerID: &srv.ID, Kind: string(kind),
			Subject: subject, Severity: "warning", Message: msg, PendingSince: now,
			FiredAt: &now, ResolvedAt: &now, Notified: true,
		}
		if err := s.db.WithContext(ctx).Omit(clause.Associations).Create(&al).Error; err != nil {
			s.log.Error("open alert", "err", err)
			return
		}
		s.openIncident(ctx, srv.AccountID, srv.ID, string(kind), subject, msg)
		s.notify(ctx, srv.AccountID, Message{Title: msg, Body: msg, Severity: "warning", URL: s.alertURL(&al)})
	}
	for _, item := range send {
		fire(item, fmt.Sprintf("%s: %s %s", srv.Name, noun, item))
	}
	if extra > 0 {
		fire("more", fmt.Sprintf("%s: %d other %ss", srv.Name, extra, noun))
	}
}

func (s *Service) noteExposureErr(ctx context.Context, srv store.Server, err error) {
	if ctx.Err() != nil {
		return
	}
	var mismatch *remote.HostKeyMismatchError
	if errors.As(err, &mismatch) {
		s.recordMismatch(ctx, srv.ID, mismatch)
		return
	}
	s.log.Warn("exposure report", "server_id", srv.ID, "err", err)
}
