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
	SpecificBinds            []string  `json:"specificBinds,omitempty"`
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
	// BaselineBinds is the previous SpecificBinds. A socket on one public
	// address (Postgres on the server's own IP) is as exposed as one on
	// every interface. It is seeded once without an alert, like the ports.
	BaselineBinds       []string `json:"baselineBinds,omitempty"`
	BaselineBindsSeeded bool     `json:"baselineBindsSeeded,omitempty"`
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
		if l.Exposure != "all" && l.Exposure != "other" {
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
		if l.Exposure == "other" {
			ex.SpecificBinds = append(ex.SpecificBinds, l.Address+" "+key)
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		ex.PublicPorts = append(ex.PublicPorts, key)
	}
	slices.Sort(ex.PublicPorts)
	slices.Sort(ex.SpecificBinds)
	ex.ListeningOnAllInterfaces = ex.PublicPorts
	ex.Gaps = mergeGaps(access.Gaps, sec.Gaps)
	ex.PasswordAuth = access.PasswordAuth
	ex.PermitRootLogin = access.PermitRootLogin
	ex.PermitEmptyPasswords = access.PermitEmptyPasswords
	ex.PubkeyAuth = access.PubkeyAuth
	ex.SSHRead = access.SSHRead
	ex.Firewall = sec.Firewall
	if sec.Firewall == "ufw active" {
		// A ufw that lets everything in by default protects nothing it does
		// not name, so it is not shown as a plain "ufw active".
		if sec.FirewallDefaultIncoming == "allow" {
			ex.Firewall = "ufw active, default allow"
		}
		for _, l := range sec.Listening {
			// Docker publishes skip ufw (UFW "unknown"), so they count as
			// uncovered too.
			if l.Exposure == "all" && (l.UFW == "unmentioned" || l.UFW == "unknown") {
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

// exposureStaleAfter is two expected collection intervals. Older evidence
// is shown as stale, never as the current state.
const exposureStaleAfter = 2 * exposureEvery

// Stale reports whether the summary is too old to describe the server now.
func (ex Exposure) Stale(now time.Time) bool {
	return ex.CheckedAt.IsZero() || now.Sub(ex.CheckedAt) > exposureStaleAfter
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
	switch ex.Firewall {
	case "none":
		add("firewall", "warning", "no firewall is active")
	case "ufw active, default allow":
		add("firewall", "warning", "the firewall allows incoming connections by default")
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
	ex, keys, accessRev, facts, ok := s.collectExposure(ctx, srv)
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
	ex = mergeExposure(prev, ex)
	if !ex.Ready {
		s.db.WithContext(ctx).Model(&store.ServerState{}).Where("server_id = ?", srv.ID).
			Updates(map[string]any{"exposure": store.MustJSON(ex), "exposure_at": now})
		state.Exposure, state.ExposureAt = store.MustJSON(ex), &now
		return
	}
	if accessRev >= proto.ReportRevision {
		s.syncKeyOccurrences(ctx, srv, keys, now)
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
		binds := ex.SpecificBinds
		if !listenOK && prev.BaselineBindsSeeded {
			ex.SpecificBinds = prev.SpecificBinds
			binds = prev.BaselineBinds
		}
		if prev.Ready && prev.BaselineSeeded {
			prevBinds := binds
			if prev.BaselineBindsSeeded {
				prevBinds = prev.BaselineBinds
			}
			s.noteAdditions(ctx, srv, append(slices.Clone(prev.BaselinePorts), prevBinds...), append(slices.Clone(ports), binds...), prev.BaselineKeys, keys)
		}
		ex.BaselineSeeded = true
		if listenOK {
			ex.BaselinePorts = ex.PublicPorts
			ex.BaselineBinds, ex.BaselineBindsSeeded = ex.SpecificBinds, true
		} else {
			ex.BaselinePorts = prev.BaselinePorts
			ex.BaselineBinds, ex.BaselineBindsSeeded = prev.BaselineBinds, prev.BaselineBindsSeeded
		}
		ex.BaselineKeys = keyIDs(keys)
	}
	raw := store.MustJSON(ex)
	updates := map[string]any{"exposure": raw, "exposure_at": now}
	if facts.SSHRead {
		updates["access_facts"] = store.MustJSON(facts)
		state.AccessFacts = store.MustJSON(facts)
	}
	s.db.WithContext(ctx).Model(&store.ServerState{}).Where("server_id = ?", srv.ID).Updates(updates)
	state.Exposure, state.ExposureAt = raw, &now
	if facts.SSHRead {
		s.reportAccessDrift(ctx, srv, facts)
	}
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

func mergeExposure(prev, next Exposure) Exposure {
	if next.Ready || prev.CheckedAt.IsZero() {
		return next
	}
	kept := prev
	kept.Ready = false
	if !hasGap(kept.Gaps, "helper") {
		kept.Gaps = append(append([]string{}, kept.Gaps...), "helper")
	}
	return kept
}

func (s *Service) collectExposure(ctx context.Context, srv store.Server) (Exposure, []keyUse, int, AccessFacts, bool) {
	ep, err := s.endpoint(ctx, srv)
	if err != nil {
		s.log.Warn("exposure report", "server_id", srv.ID, "err", err)
		return Exposure{}, nil, 0, AccessFacts{}, false
	}
	var access proto.AccessReport
	cctx, cancel := context.WithTimeout(s.netCtx(ctx), 45*time.Second)
	err = s.pool.Call(cctx, ep, &access, "access")
	cancel()
	if err != nil {
		s.noteExposureErr(ctx, srv, err)
		return Exposure{}, nil, 0, AccessFacts{}, false
	}
	var sec proto.SecurityReport
	cctx, cancel = context.WithTimeout(s.netCtx(ctx), 30*time.Second)
	err = s.pool.Call(cctx, ep, &sec, "security")
	cancel()
	if err != nil {
		s.noteExposureErr(ctx, srv, err)
		return Exposure{}, nil, 0, AccessFacts{}, false
	}
	return summarizeExposure(access, sec, s.now()), keyUses(access), access.Revision, accessFacts(access, s.now()), true
}

// keyUse is one authorized-key fingerprint and the login that holds it.
type keyUse struct {
	Fingerprint string
	User        string
	Type        string
	Comment     string
	From        string
	HasCommand  bool
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
	// One row per login and fingerprint. The same key on two logins is two
	// grants. When one login lists a key twice, the least restricted line
	// (no from=, no command=) is the one that matters.
	byID := map[string]int{}
	var out []keyUse
	for _, u := range access.Users {
		for _, k := range u.AuthorizedKeys {
			if k.Fingerprint == "" {
				continue
			}
			use := keyUse{
				Fingerprint: k.Fingerprint, User: u.Name, Type: k.Type,
				Comment: trimRunes(k.Comment, 200), From: trimRunes(k.From, 200), HasCommand: k.HasCommand,
			}
			if i, ok := byID[keyID(use)]; ok {
				if restriction(use) < restriction(out[i]) {
					out[i] = use
				}
				continue
			}
			byID[keyID(use)] = len(out)
			out = append(out, use)
		}
	}
	slices.SortFunc(out, func(a, b keyUse) int {
		if c := strings.Compare(a.Fingerprint, b.Fingerprint); c != 0 {
			return c
		}
		return strings.Compare(a.User, b.User)
	})
	return out
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// keyID is "fingerprint user", the baseline entry for one grant.
func keyID(k keyUse) string { return k.Fingerprint + " " + k.User }

// restriction ranks a key line: 0 is unrestricted.
func restriction(k keyUse) int {
	n := 0
	if k.From != "" {
		n++
	}
	if k.HasCommand {
		n++
	}
	return n
}

func keyIDs(keys []keyUse) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = keyID(k)
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
		s.oneShot(ctx, srv, rules[AlertNewPort], AlertNewPort, added(prevPorts, ports), "new listening socket")
	}
	if rules[AlertNewKey].Enabled {
		s.oneShot(ctx, srv, rules[AlertNewKey], AlertNewKey, freshKeys(prevKeys, keys), "new SSH key")
	}
}

// freshKeys lists keys not in the previous baseline. A known key added to
// another login (root, say) is new. Baselines written before logins were
// recorded hold bare fingerprints; those match the fingerprint alone once.
func freshKeys(prevKeys []string, keys []keyUse) []string {
	have := map[string]bool{}
	legacy := map[string]bool{}
	for _, p := range prevKeys {
		if strings.Contains(p, " ") {
			have[p] = true
		} else {
			legacy[p] = true
		}
	}
	var fresh []string
	for _, k := range keys {
		if !have[keyID(k)] && !legacy[k.Fingerprint] {
			fresh = append(fresh, k.Fingerprint+" on "+k.User)
		}
	}
	return fresh
}

func (s *Service) oneShot(ctx context.Context, srv store.Server, rule store.AlertRule, kind AlertKind, items []string, noun string) {
	if len(items) == 0 {
		return
	}
	now := s.now()
	fire := func(subject, msg string) {
		if len(subject) > 200 {
			subject = subject[:200]
		}
		if !s.openIncident(ctx, srv.AccountID, srv.ID, string(kind), subject, msg, true) {
			return
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
		s.notify(ctx, srv.AccountID, Message{Title: msg, Body: msg, Severity: "warning", URL: s.alertURL(&al), Kind: string(kind), ServerID: srv.ID})
	}
	// Several findings from one check are one notice. The message names each
	// one, up to eight, so they stay readable without a notice per socket.
	if len(items) > 1 {
		shown := items
		extra := 0
		if len(shown) > 8 {
			extra = len(shown) - 8
			shown = shown[:8]
		}
		msg := fmt.Sprintf("%s: %d %ss: %s", srv.Name, len(items), noun, strings.Join(shown, ", "))
		if extra > 0 {
			msg += fmt.Sprintf(", and %d more", extra)
		}
		fire("this check", msg)
		return
	}
	fire(items[0], fmt.Sprintf("%s: %s %s", srv.Name, noun, items[0]))
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
