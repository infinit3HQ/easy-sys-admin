package fleet

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// AccessUser is one login account. It has no key and no password hash.
type AccessUser struct {
	Name     string `json:"name"`
	UID      int    `json:"uid"`
	Shell    string `json:"shell"`
	Password string `json:"password"`
}

// AccessSudo is one sudo rule as the helper read it. Aliases are not expanded.
type AccessSudo struct {
	Source       string `json:"source"`
	Who          string `json:"who"`
	RunAs        string `json:"runAs"`
	Commands     string `json:"commands"`
	NoPassword   bool   `json:"noPassword"`
	Unrestricted bool   `json:"unrestricted"`
}

// AccessFacts is one access check, without keys or recent logins.
type AccessFacts struct {
	ObservedAt           time.Time    `json:"observedAt"`
	SSHRead              bool         `json:"sshRead"`
	SudoIncomplete       bool         `json:"sudoIncomplete"`
	Gaps                 []string     `json:"gaps,omitempty"`
	PasswordAuth         bool         `json:"passwordAuth"`
	PermitRootLogin      string       `json:"permitRootLogin"`
	PermitEmptyPasswords bool         `json:"permitEmptyPasswords"`
	PubkeyAuth           bool         `json:"pubkeyAuth"`
	Users                []AccessUser `json:"users"`
	Sudo                 []AccessSudo `json:"sudo"`
}

func accessFacts(rep proto.AccessReport, at time.Time) AccessFacts {
	f := AccessFacts{
		ObservedAt:           at,
		SSHRead:              rep.SSHRead,
		SudoIncomplete:       rep.SudoIncomplete,
		Gaps:                 append([]string{}, rep.Gaps...),
		PasswordAuth:         rep.PasswordAuth,
		PermitRootLogin:      rep.PermitRootLogin,
		PermitEmptyPasswords: rep.PermitEmptyPasswords,
		PubkeyAuth:           rep.PubkeyAuth,
		Users:                make([]AccessUser, 0, len(rep.Users)),
		Sudo:                 make([]AccessSudo, 0, len(rep.SudoRules)),
	}
	for _, u := range rep.Users {
		f.Users = append(f.Users, AccessUser{Name: u.Name, UID: u.UID, Shell: u.Shell, Password: u.Password})
	}
	for _, r := range rep.SudoRules {
		f.Sudo = append(f.Sudo, AccessSudo{Source: r.Source, Who: r.Who, RunAs: r.RunAs, Commands: r.Commands, NoPassword: r.NoPassword, Unrestricted: r.Unrestricted})
	}
	return f
}

// accessFactsGap says why these facts cannot be approved or compared.
// An empty result means the check was read. It does not mean the server is safe.
func accessFactsGap(f AccessFacts) string {
	switch {
	case f.ObservedAt.IsZero():
		return "no access check has been recorded"
	case !f.SSHRead:
		return "SSH settings were not read"
	case f.SudoIncomplete:
		return "sudoers was only partly read"
	case len(f.Gaps) > 0:
		return "the check left gaps: " + strings.Join(f.Gaps, ", ")
	case len(f.Users) == 0:
		return "no login accounts were read"
	default:
		return ""
	}
}

// ApproveAccessBaseline stores the last recorded access check. It does not
// call the server. A stale or partial check is refused.
func (s *Service) ApproveAccessBaseline(ctx context.Context, a Actor, serverID int64, reason string) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 200 {
		return invalid("reason", "say why, in at most 200 characters")
	}
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return err
	}
	var state store.ServerState
	if err := s.db.WithContext(ctx).Where("server_id = ?", srv.ID).Take(&state).Error; err != nil {
		return err
	}
	var facts AccessFacts
	if err := state.AccessFacts.Decode(&facts); err != nil || accessFactsGap(facts) != "" {
		why := accessFactsGap(facts)
		if why == "" {
			why = "the recorded access check could not be read"
		}
		return invalid("server", "cannot approve (%s)", why)
	}
	if s.now().Sub(facts.ObservedAt) > exposureStaleAfter {
		return invalid("server", "the recorded access check is older than %s, so it cannot be approved", exposureStaleAfter)
	}
	row := store.AccessBaseline{
		ServerID: srv.ID, AccountID: srv.AccountID, Facts: store.MustJSON(facts),
		Reason: reason, ApprovedAt: s.now(), ApprovedBy: a.UserID,
	}
	if err := s.db.WithContext(ctx).Omit("Server").Save(&row).Error; err != nil {
		return err
	}
	s.audit(ctx, nil, a, "access.baseline", srv.Name, map[string]any{
		"users": len(facts.Users), "sudo": len(facts.Sudo), "reason": reason,
	})
	return nil
}

type accessDrift struct {
	Subject string
	Message string
}

func compareAccess(serverName string, base, cur AccessFacts) []accessDrift {
	var out []accessDrift
	add := func(subject, message string) {
		out = append(out, accessDrift{Subject: subject, Message: serverName + ": " + message})
	}
	if base.PasswordAuth != cur.PasswordAuth {
		add("ssh password login", fmt.Sprintf("SSH password login changed from %s to %s", onOff(base.PasswordAuth), onOff(cur.PasswordAuth)))
	}
	if base.PermitRootLogin != cur.PermitRootLogin {
		add("ssh root login", fmt.Sprintf("SSH root login changed from %q to %q", base.PermitRootLogin, cur.PermitRootLogin))
	}
	if base.PermitEmptyPasswords != cur.PermitEmptyPasswords {
		add("ssh empty passwords", fmt.Sprintf("SSH empty passwords changed from %s to %s", onOff(base.PermitEmptyPasswords), onOff(cur.PermitEmptyPasswords)))
	}
	if base.PubkeyAuth != cur.PubkeyAuth {
		add("ssh public keys", fmt.Sprintf("SSH public-key login changed from %s to %s", onOff(base.PubkeyAuth), onOff(cur.PubkeyAuth)))
	}
	oldUsers := map[string]AccessUser{}
	for _, u := range base.Users {
		oldUsers[u.Name] = u
	}
	seen := map[string]bool{}
	for _, u := range cur.Users {
		seen[u.Name] = true
		old, ok := oldUsers[u.Name]
		if !ok {
			add("user "+u.Name, "login "+u.Name+" was not in the approved list")
			continue
		}
		if old.UID != u.UID || old.Shell != u.Shell || old.Password != u.Password {
			add("user "+u.Name+" changed", fmt.Sprintf("login %s changed (uid %d to %d, shell %s to %s, password %s to %s)", u.Name, old.UID, u.UID, old.Shell, u.Shell, old.Password, u.Password))
		}
	}
	for name := range oldUsers {
		if !seen[name] {
			add("user "+name+" missing", "approved login "+name+" was not in the accounts checked this time")
		}
	}
	oldSudo := map[string]int{}
	for _, r := range base.Sudo {
		oldSudo[sudoLine(r)]++
	}
	curSudo := map[string]int{}
	for _, r := range cur.Sudo {
		curSudo[sudoLine(r)]++
	}
	for line, n := range curSudo {
		if oldSudo[line] >= n {
			continue
		}
		add("sudo "+shortSudo(line), "a sudo rule was not in the approved list ("+shortSudo(line)+")")
	}
	for line, n := range oldSudo {
		if curSudo[line] >= n {
			continue
		}
		add("sudo missing "+shortSudo(line), "an approved sudo rule was not in the rules checked this time ("+shortSudo(line)+")")
	}
	return out
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func sudoLine(r AccessSudo) string {
	return strings.Join([]string{r.Source, r.Who, r.RunAs, r.Commands, onOff(r.NoPassword), onOff(r.Unrestricted)}, "\t")
}

func shortSudo(line string) string {
	parts := strings.Split(line, "\t")
	if len(parts) < 2 {
		return line
	}
	who := parts[1]
	if len(who) > 80 {
		who = who[:80]
	}
	return who + " in " + parts[0]
}

// reportAccessDrift opens a notice for each difference from the approved
// check. A partial new check opens nothing and closes nothing.
func (s *Service) reportAccessDrift(ctx context.Context, srv store.Server, cur AccessFacts) {
	if accessFactsGap(cur) != "" {
		return
	}
	var base store.AccessBaseline
	if err := s.db.WithContext(ctx).Where("server_id = ?", srv.ID).Take(&base).Error; err != nil {
		return
	}
	var approved AccessFacts
	if err := base.Facts.Decode(&approved); err != nil {
		return
	}
	for _, d := range compareAccess(srv.Name, approved, cur) {
		s.openIncident(ctx, srv.AccountID, srv.ID, "access", d.Subject, d.Message, false)
	}
}
