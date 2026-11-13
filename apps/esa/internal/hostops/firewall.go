package hostops

import (
	"context"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// ---- Firewall --------------------------------------------------------------

const (
	ufwProfileDir     = "/etc/ufw/applications.d"
	ufwDefaultsFile   = "/etc/default/ufw"
	maxUFWProfileFile = 64 << 10
	maxUFWProfiles    = 200
	maxUFWRules       = 1000
	maxShownUFWRules  = 40
)

// readFirewall fills the firewall fields and marks each listener with the
// ufw rule that covers it.
func (h *Host) readFirewall(ctx context.Context, rep *proto.SecurityReport) {
	out, err := h.output(ctx, 10*time.Second, "ufw", "status", "verbose")
	if err == nil && strings.Contains(out, "Status: active") {
		rep.Firewall = "ufw active"
		rep.FirewallRules = ufwRules(out)
		rep.FirewallDefaultIncoming = ufwDefaultIncoming(out)
		if rep.FirewallDefaultIncoming == "" {
			rep.FirewallDefaultIncoming = h.ufwDefaultFromFile()
		}
		if rep.FirewallDefaultIncoming == "" {
			rep.Gaps = append(rep.Gaps, "ufw default policy")
		}
		if applyUFW(rep.Listening, ufwRuleLines(out, maxUFWRules), h.ufwProfiles()) {
			rep.Gaps = append(rep.Gaps, "ufw app profiles")
		}
		return
	}
	if err == nil && strings.Contains(out, "Status: inactive") {
		rep.Firewall = "none"
		return
	}
	if out, err := h.output(ctx, 10*time.Second, "nft", "list", "ruleset"); err == nil && strings.Contains(out, "chain") {
		rep.Firewall = "nftables rules"
	} else if err == nil {
		rep.Firewall = "none"
	} else {
		rep.Firewall = "unknown"
		rep.Gaps = append(rep.Gaps, "firewall")
	}
}

// ufwRules returns the rule lines of an active `ufw status`, capped at 40.
func ufwRules(out string) []string {
	rules := ufwRuleLines(out, maxShownUFWRules)
	for i, l := range rules {
		if len(l) > 160 {
			rules[i] = l[:160]
		}
	}
	return rules
}

// ufwRuleLines returns up to max rule lines after the "--" header line.
func ufwRuleLines(out string, max int) []string {
	lines := strings.Split(out, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "--") {
			start = i + 1
			break
		}
	}
	rules := []string{}
	if start < 0 {
		return rules
	}
	for _, l := range lines[start:] {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		rules = append(rules, l)
		if len(rules) == max {
			break
		}
	}
	return rules
}

// ufwDefaultIncoming reads "Default: deny (incoming), ..." from
// `ufw status verbose`. It returns deny, reject, allow, or "".
func ufwDefaultIncoming(out string) string {
	for _, line := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Default:")
		if !ok {
			continue
		}
		for _, part := range strings.Split(rest, ",") {
			if !strings.Contains(part, "(incoming)") {
				continue
			}
			f := strings.Fields(part)
			if len(f) > 0 {
				return oneOf(f[0], "", "deny", "reject", "allow")
			}
		}
	}
	return ""
}

// ufwDefaultFromFile reads DEFAULT_INPUT_POLICY from /etc/default/ufw.
func (h *Host) ufwDefaultFromFile() string {
	raw, _, err := h.readRegular(ufwDefaultsFile, 64<<10)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		v, ok := strings.CutPrefix(strings.TrimSpace(line), "DEFAULT_INPUT_POLICY=")
		if !ok {
			continue
		}
		switch strings.ToUpper(strings.Trim(strings.TrimSpace(v), `"'`)) {
		case "DROP":
			return "deny"
		case "REJECT":
			return "reject"
		case "ACCEPT":
			return "allow"
		}
		return ""
	}
	return ""
}

// ufwPorts is one port list from a rule or an app profile. An empty proto
// means tcp and udp.
type ufwPorts struct {
	ranges [][2]int
	proto  string
}

func (p ufwPorts) covers(port int, protoName string) bool {
	if p.proto != "" && p.proto != protoName {
		return false
	}
	for _, r := range p.ranges {
		if port >= r[0] && port <= r[1] {
			return true
		}
	}
	return false
}

// parseUFWPorts reads "22", "22/tcp", "80,443/tcp", or "6000:6007/udp".
func parseUFWPorts(s string) (ufwPorts, bool) {
	list, protoName, hasProto := strings.Cut(s, "/")
	out := ufwPorts{}
	if hasProto {
		if protoName != "tcp" && protoName != "udp" {
			return ufwPorts{}, false
		}
		out.proto = protoName
	}
	for _, item := range strings.Split(list, ",") {
		lo, hi, isRange := strings.Cut(item, ":")
		a, err := strconv.Atoi(lo)
		if err != nil || a < 1 || a > 65535 {
			return ufwPorts{}, false
		}
		b := a
		if isRange {
			if b, err = strconv.Atoi(hi); err != nil || b < a || b > 65535 {
				return ufwPorts{}, false
			}
		}
		out.ranges = append(out.ranges, [2]int{a, b})
	}
	return out, true
}

// ufwRule is one incoming rule. all is true when the rule names no port.
type ufwRule struct {
	all    bool
	ports  []ufwPorts
	action string // allow | deny
}

func (r ufwRule) covers(port int, protoName string) bool {
	if r.all {
		return true
	}
	for _, p := range r.ports {
		if p.covers(port, protoName) {
			return true
		}
	}
	return false
}

// parseUFWRule reads one rule line. ok is false for lines that are not an
// incoming rule. unresolved is true for an app profile that is not known.
func parseUFWRule(line string, profiles map[string][]ufwPorts) (rule ufwRule, ok, unresolved bool) {
	f := strings.Fields(line)
	idx := -1
	for i, field := range f {
		switch strings.ToUpper(field) {
		case "ALLOW", "LIMIT":
			rule.action = "allow"
		case "DENY", "REJECT":
			rule.action = "deny"
		}
		if rule.action != "" {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return ufwRule{}, false, false
	}
	if idx+1 < len(f) {
		if dir := strings.ToUpper(f[idx+1]); dir == "OUT" || dir == "FWD" {
			return ufwRule{}, false, false
		}
	}
	var to []string
	for i := 0; i < idx; i++ {
		switch {
		case f[i] == "(v6)":
		case f[i] == "on" && i+1 < idx:
			i++
		default:
			to = append(to, f[i])
		}
	}
	if len(to) == 0 {
		return ufwRule{}, false, false
	}
	if ports, found := profiles[strings.Join(to, " ")]; found {
		rule.ports = ports
		return rule, true, false
	}
	addresses := true
	for _, t := range to {
		if p, isPort := parseUFWPorts(t); isPort {
			rule.ports = append(rule.ports, p)
			continue
		}
		if !ufwAddress(t) {
			addresses = false
		}
	}
	if len(rule.ports) > 0 {
		return rule, true, false
	}
	if addresses {
		rule.all = true
		return rule, true, false
	}
	// "10.0.0.5 OpenSSH": a destination address and then a profile.
	if ufwAddress(to[0]) {
		if ports, found := profiles[strings.Join(to[1:], " ")]; found {
			rule.ports = ports
			return rule, true, false
		}
	}
	return ufwRule{}, false, true
}

func ufwAddress(s string) bool {
	if s == "Anywhere" {
		return true
	}
	if net.ParseIP(s) != nil {
		return true
	}
	_, _, err := net.ParseCIDR(s)
	return err == nil
}

// parseUFW reads rule lines. unresolved is true when a rule names an app
// profile that is not in profiles.
func parseUFW(lines []string, profiles map[string][]ufwPorts) ([]ufwRule, bool) {
	var rules []ufwRule
	unresolved := false
	for _, line := range lines {
		r, ok, bad := parseUFWRule(line, profiles)
		unresolved = unresolved || bad
		if ok {
			rules = append(rules, r)
		}
	}
	return rules, unresolved
}

// applyUFW marks each listener allow, deny, or unmentioned from ufw rules.
// An allow wins over a deny for the same port. It reports whether a rule
// named an app profile that could not be resolved.
func applyUFW(ls []proto.Listener, lines []string, profiles map[string][]ufwPorts) bool {
	rules, unresolved := parseUFW(lines, profiles)
	for i := range ls {
		ls[i].UFW = ufwFor(ls[i].Port, ls[i].Proto, rules)
	}
	return unresolved
}

func ufwFor(port int, protoName string, rules []ufwRule) string {
	p := baseProto(protoName)
	allow, deny := false, false
	for _, r := range rules {
		if !r.covers(port, p) {
			continue
		}
		switch r.action {
		case "allow":
			allow = true
		case "deny":
			deny = true
		}
	}
	if allow {
		return "allow"
	}
	if deny {
		return "deny"
	}
	return "unmentioned"
}

func baseProto(p string) string {
	switch {
	case strings.HasPrefix(p, "tcp"):
		return "tcp"
	case strings.HasPrefix(p, "udp"):
		return "udp"
	}
	return p
}

// ufwProfiles reads app profiles from /etc/ufw/applications.d. Only
// regular files are read; links and other files are skipped.
func (h *Host) ufwProfiles() map[string][]ufwPorts {
	out := map[string][]ufwPorts{}
	entries, err := os.ReadDir(h.path(ufwProfileDir))
	if err != nil {
		return out
	}
	for i, e := range entries {
		if i == maxUFWProfiles {
			break
		}
		if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		raw, _, err := h.readRegular(ufwProfileDir+"/"+e.Name(), maxUFWProfileFile)
		if err != nil {
			continue
		}
		for name, ports := range parseUFWProfiles(string(raw)) {
			out[name] = ports
		}
	}
	return out
}

// parseUFWProfiles reads "[Name]" sections and their "ports=" line, for
// example "ports=80,443/tcp" or "ports=53|5353/udp".
func parseUFWProfiles(text string) map[string][]ufwPorts {
	out := map[string][]ufwPorts{}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		v, ok := strings.CutPrefix(line, "ports=")
		if !ok || section == "" {
			continue
		}
		var ports []ufwPorts
		valid := true
		for _, spec := range strings.Split(v, "|") {
			p, ok := parseUFWPorts(strings.TrimSpace(spec))
			if !ok {
				valid = false
				break
			}
			ports = append(ports, p)
		}
		if valid && len(ports) > 0 {
			out[section] = ports
		}
	}
	return out
}

// ---- Docker publishes ------------------------------------------------------

// publication is one host port a running container publishes.
type publication struct {
	Port    int
	Proto   string
	Address string
	Names   string
}

func (p publication) key() string { return strconv.Itoa(p.Port) + "/" + p.Proto }

// publishedPorts lists the ports running containers publish. Docker
// forwards these with its own rules, so they can skip ufw.
func (h *Host) publishedPorts(ctx context.Context) ([]publication, error) {
	if h.Docker == nil {
		return nil, nil
	}
	list, err := h.Docker.containers(ctx)
	if err != nil {
		return nil, err
	}
	return publicationsFrom(list), nil
}

func publicationsFrom(list []dockerContainer) []publication {
	found := map[string]*publication{}
	var order []string
	for _, c := range list {
		if c.State != "running" {
			continue
		}
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		if name == "" || len(c.ID) < 12 {
			continue
		}
		for _, p := range c.Ports {
			if p.PublicPort <= 0 || p.PublicPort > 65535 || (p.Type != "tcp" && p.Type != "udp") {
				continue
			}
			addr := p.IP
			if addr == "" {
				addr = "0.0.0.0"
			}
			k := strconv.Itoa(p.PublicPort) + "/" + p.Type + " " + addr
			pub, ok := found[k]
			if !ok {
				pub = &publication{Port: p.PublicPort, Proto: p.Type, Address: addr}
				found[k] = pub
				order = append(order, k)
			}
			pub.Names = joinNames(pub.Names, name)
		}
	}
	out := make([]publication, 0, len(order))
	for _, k := range order {
		out = append(out, *found[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		if out[i].Proto != out[j].Proto {
			return out[i].Proto < out[j].Proto
		}
		return out[i].Address < out[j].Address
	})
	return out
}

// joinNames adds name to a comma list of at most three names.
func joinNames(list, name string) string {
	if list == "" {
		return name
	}
	names := strings.Split(list, ", ")
	for _, n := range names {
		if n == name {
			return list
		}
	}
	if len(names) >= 3 {
		return list
	}
	return list + ", " + name
}

// publishedNames maps "port/proto" to the containers that publish it.
func publishedNames(pubs []publication) map[string]string {
	out := map[string]string{}
	for _, p := range pubs {
		for _, n := range strings.Split(p.Names, ", ") {
			out[p.key()] = joinNames(out[p.key()], n)
		}
	}
	return out
}

func attachContainers(ls []proto.Listener, pubs []publication) {
	names := publishedNames(pubs)
	if len(names) == 0 {
		return
	}
	for i := range ls {
		if ls[i].Process != "docker-proxy" {
			continue
		}
		ls[i].Container = names[strconv.Itoa(ls[i].Port)+"/"+baseProto(ls[i].Proto)]
	}
}

// addMissingPublications lists a published port that ss does not show, as
// with userland-proxy off. A port ss already lists is not added again.
func addMissingPublications(ls []proto.Listener, pubs []publication) []proto.Listener {
	have := map[string]bool{}
	for _, l := range ls {
		have[strconv.Itoa(l.Port)+"/"+baseProto(l.Proto)] = true
	}
	added := map[string]bool{}
	for _, p := range pubs {
		if have[p.key()] {
			continue
		}
		exposure := listenExposure(strings.Trim(p.Address, "[]"))
		if added[p.key()+" "+exposure] {
			continue
		}
		added[p.key()+" "+exposure] = true
		ls = append(ls, proto.Listener{
			Proto: p.Proto, Address: p.Address, Port: p.Port, Process: "docker",
			Container: p.Names, Exposure: exposure, UFW: "unknown",
		})
	}
	sort.SliceStable(ls, func(i, j int) bool {
		if ls[i].Port != ls[j].Port {
			return ls[i].Port < ls[j].Port
		}
		return ls[i].Proto < ls[j].Proto
	})
	return ls
}

// markDockerUnknown refuses to treat a ufw rule as protection for a port
// Docker publishes. Published traffic is forwarded by Docker's own rules
// and does not pass ufw's input rules. Call it after applyUFW.
func markDockerUnknown(ls []proto.Listener, pubs []publication) {
	names := publishedNames(pubs)
	if len(names) == 0 {
		return
	}
	for i := range ls {
		if _, ok := names[strconv.Itoa(ls[i].Port)+"/"+baseProto(ls[i].Proto)]; ok {
			ls[i].UFW = "unknown"
		}
	}
}
