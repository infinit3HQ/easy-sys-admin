package fleet

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// IdentityView is an owner of keys: the name used as a key label, what it
// is, and how many grants carry its keys on servers the actor can see.
type IdentityView struct {
	Name    string
	Kind    string
	Status  string
	Note    string
	Keys    int
	Grants  int
	Servers int
}

// keyEvidenceGaps are report gaps that make a server's key list incomplete.
func keyEvidenceGaps(gaps []string) []string {
	var out []string
	for _, g := range gaps {
		if strings.HasPrefix(g, "authorized_keys") || strings.Contains(g, "AuthorizedKeys") ||
			strings.HasPrefix(g, "ssh") || g == "helper" {
			out = append(out, g)
		}
	}
	return out
}

// keyCoverage says why a server's recorded keys cannot be trusted as the
// current list, or "" when they can: a ready, fresh report with no gaps in
// the key sources.
func keyCoverage(v ServerView, now time.Time) string {
	ex := v.Exposure
	switch {
	case ex == nil:
		return "no access report yet"
	case !ex.Ready:
		return "the helper is too old to read keys; reinstall it"
	case ex.Stale(now):
		return "no successful check since " + ex.CheckedAt.UTC().Format("Jan 2 15:04 MST")
	}
	if gaps := keyEvidenceGaps(ex.Gaps); len(gaps) > 0 {
		return "not read: " + strings.Join(gaps, ", ")
	}
	return ""
}

// ListIdentities returns every owner named in this space: the key labels,
// plus owners recorded without a key yet.
func (s *Service) ListIdentities(ctx context.Context, a Actor) ([]IdentityView, error) {
	keys, err := s.ListKeys(ctx, a)
	if err != nil {
		return nil, err
	}
	var rows []store.AccessIdentity
	if err := s.db.WithContext(ctx).Where("account_id = ?", a.AccountID).Find(&rows).Error; err != nil {
		return nil, err
	}
	byName := map[string]*IdentityView{}
	get := func(name string) *IdentityView {
		v, ok := byName[name]
		if !ok {
			v = &IdentityView{Name: name, Kind: "person", Status: "active"}
			byName[name] = v
		}
		return v
	}
	for _, r := range rows {
		v := get(r.Name)
		v.Kind, v.Status, v.Note = r.Kind, r.Status, r.Note
	}
	servers := map[string]map[int64]bool{}
	for _, k := range keys {
		if k.Label == "" {
			continue
		}
		v := get(k.Label)
		v.Keys++
		v.Grants += len(k.Uses)
		if servers[k.Label] == nil {
			servers[k.Label] = map[int64]bool{}
		}
		for _, u := range k.Uses {
			servers[k.Label][u.ServerID] = true
		}
	}
	out := make([]IdentityView, 0, len(byName))
	for name, v := range byName {
		v.Servers = len(servers[name])
		out = append(out, *v)
	}
	slices.SortFunc(out, func(x, y IdentityView) int { return strings.Compare(x.Name, y.Name) })
	return out, nil
}

// SaveIdentity records what an owner is and their status. It does not
// change any server.
func (s *Service) SaveIdentity(ctx context.Context, a Actor, in IdentityView) (IdentityView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return IdentityView{}, err
	}
	name := strings.Join(strings.Fields(in.Name), " ")
	if name == "" || utf8.RuneCountInString(name) > 80 {
		return IdentityView{}, invalid("name", "must be 1 to 80 characters")
	}
	if !slices.Contains([]string{"person", "service", "shared"}, in.Kind) {
		return IdentityView{}, invalid("kind", "must be person, service, or shared")
	}
	if !slices.Contains([]string{"active", "leaving", "left"}, in.Status) {
		return IdentityView{}, invalid("status", "must be active, leaving, or left")
	}
	note := strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(note) > 500 {
		return IdentityView{}, invalid("note", "must be at most 500 characters")
	}
	var row store.AccessIdentity
	err := s.db.WithContext(ctx).Where("account_id = ? AND name = ?", a.AccountID, name).Take(&row).Error
	row.AccountID, row.Name, row.Kind, row.Status, row.Note, row.UpdatedAt = a.AccountID, name, in.Kind, in.Status, note, s.now()
	if err != nil {
		err = s.db.WithContext(ctx).Omit("Account").Create(&row).Error
	} else {
		err = s.db.WithContext(ctx).Omit("Account").Save(&row).Error
	}
	if err != nil {
		return IdentityView{}, mapStore(err)
	}
	s.audit(ctx, nil, a, "identity.save", name, map[string]any{"kind": in.Kind, "status": in.Status})
	all, err := s.ListIdentities(ctx, a)
	if err != nil {
		return IdentityView{}, err
	}
	for _, v := range all {
		if v.Name == name {
			return v, nil
		}
	}
	return IdentityView{Name: name, Kind: in.Kind, Status: in.Status, Note: note}, nil
}

// OffboardingGrant is one place a person's key was last seen.
type OffboardingGrant struct {
	ServerID    int64
	Server      string
	User        string
	Fingerprint string
	KeyType     string
	From        string
	HasCommand  bool
	ObservedAt  time.Time
	// Unchecked is why this server's key list is not current; empty when
	// it is.
	Unchecked string
}

// OffboardingGap is a server whose keys this report cannot vouch for.
type OffboardingGap struct {
	ServerID int64
	Server   string
	Reason   string
}

// OffboardingReport lists where an owner's known keys were last seen. It
// is complete only when every visible server has a fresh, gap-free key
// list and the actor sees the whole space. It never says access was
// removed: other ways in (passwords, certificates, other key files) are
// outside it.
type OffboardingReport struct {
	Identity    IdentityView
	GeneratedAt time.Time
	Statement   string
	Complete    bool
	Partial     bool
	Grants      []OffboardingGrant
	Unchecked   []OffboardingGap
	// SharedKeys are this owner's fingerprints that also open other
	// logins, so removing them affects someone else.
	SharedKeys []string
	Keys       []KeyView
}

const offboardingStatement = "Where this owner's known keys were last seen in the authorized_keys files esa reads. " +
	"It does not prove access was removed: passwords, SSH certificates, other key files, and servers esa cannot check are outside it."

// Offboarding builds the report for one owner name.
func (s *Service) Offboarding(ctx context.Context, a Actor, name string) (OffboardingReport, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return OffboardingReport{}, invalid("label", "is required")
	}
	ids, err := s.ListIdentities(ctx, a)
	if err != nil {
		return OffboardingReport{}, err
	}
	rep := OffboardingReport{Identity: IdentityView{Name: name, Kind: "person", Status: "active"}, GeneratedAt: s.now(), Statement: offboardingStatement,
		Grants: []OffboardingGrant{}, Unchecked: []OffboardingGap{}, SharedKeys: []string{}, Keys: []KeyView{}}
	for _, v := range ids {
		if v.Name == name {
			rep.Identity = v
		}
	}
	views, err := s.serverViews(ctx, a, nil)
	if err != nil {
		return OffboardingReport{}, err
	}
	now := s.now()
	coverage := map[int64]string{}
	for _, v := range views {
		if v.Access != AccessOwn {
			rep.Partial = true
		}
		if v.Enrollment != "ready" {
			continue
		}
		if why := keyCoverage(v, now); why != "" {
			coverage[v.ID] = why
			rep.Unchecked = append(rep.Unchecked, OffboardingGap{ServerID: v.ID, Server: v.Name, Reason: why})
		}
	}
	keys, err := s.ListKeys(ctx, a)
	if err != nil {
		return OffboardingReport{}, err
	}
	for _, k := range keys {
		if k.Label != name {
			continue
		}
		rep.Keys = append(rep.Keys, k)
		if len(k.Uses) > 1 {
			rep.SharedKeys = append(rep.SharedKeys, k.Fingerprint)
		}
		for _, u := range k.Uses {
			rep.Grants = append(rep.Grants, OffboardingGrant{
				ServerID: u.ServerID, Server: u.ServerName, User: u.User, Fingerprint: k.Fingerprint, KeyType: k.KeyType,
				From: u.From, HasCommand: u.HasCommand, ObservedAt: u.ObservedAt, Unchecked: coverage[u.ServerID],
			})
		}
	}
	rep.Complete = len(rep.Unchecked) == 0 && !rep.Partial
	return rep, nil
}

// Markdown renders the report for a person to keep or send. Server text
// (comments) is left out; fingerprints and logins are what matter.
func (r OffboardingReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Offboarding report: %s\n\n", r.Identity.Name)
	fmt.Fprintf(&b, "Generated %s. Owner kind: %s. Status: %s.\n\n", r.GeneratedAt.UTC().Format(time.RFC3339), r.Identity.Kind, r.Identity.Status)
	b.WriteString(r.Statement + "\n\n")
	switch {
	case r.Partial:
		b.WriteString("**Partial view.** Some servers belong to another organization; this report covers only what you can see.\n\n")
	case !r.Complete:
		b.WriteString("**Incomplete.** Some servers could not be checked; see below.\n\n")
	}
	b.WriteString("## Known grants\n\n")
	if len(r.Grants) == 0 {
		b.WriteString("No known key of this owner was seen in the files esa read.\n\n")
	} else {
		b.WriteString("| Server | Login | Fingerprint | Restrictions | Last seen | Evidence |\n| --- | --- | --- | --- | --- | --- |\n")
		for _, g := range r.Grants {
			restr := "none"
			var parts []string
			if g.From != "" {
				parts = append(parts, "from="+g.From)
			}
			if g.HasCommand {
				parts = append(parts, "forced command")
			}
			if len(parts) > 0 {
				restr = strings.Join(parts, ", ")
			}
			evidence := "current"
			if g.Unchecked != "" {
				evidence = g.Unchecked
			}
			fmt.Fprintf(&b, "| %s | %s | `%s` | %s | %s | %s |\n", mdCell(g.Server), mdCell(g.User), g.Fingerprint, mdCell(restr),
				g.ObservedAt.UTC().Format("2006-01-02 15:04 MST"), mdCell(evidence))
		}
		b.WriteString("\n")
	}
	if len(r.SharedKeys) > 0 {
		b.WriteString("## Keys that open more than one login\n\nRemoving these affects every login listed above for them:\n\n")
		for _, fp := range r.SharedKeys {
			fmt.Fprintf(&b, "- `%s`\n", fp)
		}
		b.WriteString("\n")
	}
	if len(r.Unchecked) > 0 {
		b.WriteString("## Servers esa could not check\n\n")
		for _, g := range r.Unchecked {
			fmt.Fprintf(&b, "- %s: %s\n", mdCell(g.Server), mdCell(g.Reason))
		}
		b.WriteString("\n")
	}
	b.WriteString("## Checklist\n\n- [ ] Remove each key above from its login with your own tools.\n- [ ] Check the servers esa could not check by hand.\n- [ ] Check passwords, SSH certificates, VPN and cloud console access.\n- [ ] Run this report again; a key esa no longer sees is shown as no longer observed, not as revoked.\n")
	return b.String()
}

func mdCell(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ", "\r", " ").Replace(s)
}
