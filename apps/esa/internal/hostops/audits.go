package hostops

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// ---- Access ------------------------------------------------------------------

var noLoginShells = map[string]bool{
	"/usr/sbin/nologin": true, "/sbin/nologin": true, "/bin/false": true, "/usr/bin/false": true, "/bin/sync": true,
}

// Access reports who can log in and how.
func (h *Host) Access(ctx context.Context) (proto.AccessReport, error) {
	rep := proto.AccessReport{
		Revision: proto.ReportRevision, Users: []proto.LocalUser{}, Groups: map[string][]string{},
		Logins: []proto.Login{}, ListenPorts: []int{}, ListenAddresses: []string{}, SudoRules: []proto.SudoRule{},
		Gaps: []string{},
	}
	passwords := h.passwordStates()
	if passwords == nil {
		rep.Gaps = append(rep.Gaps, "password state")
	}

	raw, err := h.readFile("/etc/passwd")
	if err != nil {
		return rep, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 7 || noLoginShells[f[6]] || f[6] == "" {
			continue
		}
		uid, _ := strconv.Atoi(f[2])
		pw := "unknown"
		if passwords != nil {
			if st, ok := passwords[f[0]]; ok {
				pw = st
			}
		}
		u := proto.LocalUser{Name: f[0], UID: uid, Home: f[5], Shell: f[6], Password: pw, AuthorizedKeys: []proto.AuthorizedKey{}}
		for _, name := range []string{"authorized_keys", "authorized_keys2"} {
			keys, gaps := h.authorizedKeys(filepath.Join(f[5], ".ssh", name))
			u.AuthorizedKeys = append(u.AuthorizedKeys, keys...)
			for _, g := range gaps {
				rep.Gaps = addGap(rep.Gaps, g)
			}
		}
		rep.Users = append(rep.Users, u)
	}
	rep.SudoRules, rep.SudoIncomplete = h.sudoRules()

	if raw, err := h.readFile("/etc/group"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			f := strings.Split(line, ":")
			if len(f) < 4 {
				continue
			}
			switch f[0] {
			case "sudo", "wheel", "admin", "docker", "adm":
				members := []string{}
				for _, m := range strings.Split(f[3], ",") {
					if m = strings.TrimSpace(m); m != "" {
						members = append(members, m)
					}
				}
				rep.Groups[f[0]] = members
			}
		}
	}

	if out, err := h.output(ctx, 15*time.Second, "last", "--time-format", "iso", "-n", "20", "-w"); err == nil {
		rep.Logins = parseLast(out)
	}
	if out, err := h.output(ctx, 30*time.Second, "journalctl", "-u", "ssh", "-u", "sshd", "--since", "-24h", "-o", "cat", "--no-pager"); err == nil {
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "Failed password") || strings.Contains(l, "Invalid user") || strings.Contains(l, "Failed publickey") {
				rep.FailedSSH24h++
			}
		}
	}
	if out, err := h.output(ctx, 10*time.Second, "sshd", "-T"); err != nil {
		rep.Gaps = append(rep.Gaps, "ssh settings")
	} else {
		rep.SSHRead = true
		for _, l := range strings.Split(out, "\n") {
			k, v, _ := strings.Cut(l, " ")
			switch k {
			case "passwordauthentication":
				rep.PasswordAuth = v == "yes"
			case "permitrootlogin":
				rep.PermitRootLogin = v
			case "permitemptypasswords":
				rep.PermitEmptyPasswords = v == "yes"
			case "pubkeyauthentication":
				rep.PubkeyAuth = v == "yes"
			case "x11forwarding":
				rep.X11Forwarding = v == "yes"
			case "allowtcpforwarding":
				rep.AllowTcpForwarding = v
			case "kbdinteractiveauthentication":
				rep.KbdInteractive = v == "yes"
			case "port":
				if p, err := strconv.Atoi(v); err == nil && len(rep.ListenPorts) < 8 {
					rep.ListenPorts = append(rep.ListenPorts, p)
				}
			case "listenaddress":
				if v != "" && len(rep.ListenAddresses) < 16 {
					rep.ListenAddresses = append(rep.ListenAddresses, v)
				}
			case "authorizedkeysfile":
				if !defaultAuthorizedKeysFile(v) {
					rep.Gaps = addGap(rep.Gaps, "custom AuthorizedKeysFile")
				}
			}
		}
		// sshd -T prints the settings outside any Match block. A Match block
		// can change them for some users or addresses, so they are not the
		// whole picture.
		if h.sshMatchBlocks() {
			rep.Gaps = addGap(rep.Gaps, "ssh Match blocks")
		}
	}
	return rep, nil
}

// addGap appends name once.
func addGap(gaps []string, name string) []string {
	for _, g := range gaps {
		if g == name {
			return gaps
		}
	}
	return append(gaps, name)
}

// defaultAuthorizedKeysFile reports whether sshd reads keys only from the
// files Access reads.
func defaultAuthorizedKeysFile(v string) bool {
	f := strings.Fields(v)
	for i, p := range f {
		f[i] = strings.TrimPrefix(p, "%h/")
	}
	switch len(f) {
	case 1:
		return f[0] == ".ssh/authorized_keys"
	case 2:
		return f[0] == ".ssh/authorized_keys" && f[1] == ".ssh/authorized_keys2"
	}
	return false
}

const (
	sshdConfig     = "/etc/ssh/sshd_config"
	sshdConfigDir  = "/etc/ssh/sshd_config.d"
	maxSSHDConfig  = 256 << 10
	maxSSHDConfigs = 64
)

// sshMatchBlocks reports whether sshd_config or a .conf file in
// sshd_config.d has a Match line. It does not evaluate Match. A file that
// cannot be read counts as true, because a Match block cannot be ruled out.
func (h *Host) sshMatchBlocks() bool {
	files := []string{sshdConfig}
	if entries, err := os.ReadDir(h.path(sshdConfigDir)); err == nil {
		names := []string{}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".conf") {
				names = append(names, e.Name())
			}
		}
		if len(names) > maxSSHDConfigs {
			return true
		}
		sort.Strings(names)
		for _, n := range names {
			files = append(files, sshdConfigDir+"/"+n)
		}
	} else if !os.IsNotExist(err) {
		return true
	}
	for _, path := range files {
		raw, trunc, err := h.readRegular(path, maxSSHDConfig)
		if err != nil || trunc || hasMatchLine(string(raw)) {
			return true
		}
	}
	return false
}

func hasMatchLine(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		f := strings.FieldsFunc(line, func(r rune) bool { return r == ' ' || r == '\t' || r == '=' || r == '\r' })
		if len(f) > 0 && strings.EqualFold(f[0], "match") {
			return true
		}
	}
	return false
}

// passwordStates classifies /etc/shadow. The hash never leaves this function.
// A nil map means the file could not be read.
func (h *Host) passwordStates() map[string]string {
	raw, _, err := h.readRegular("/etc/shadow", 1<<20)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		name, field, ok := strings.Cut(line, ":")
		if !ok || name == "" {
			continue
		}
		out[name] = classifyPassword(field)
	}
	return out
}

// classifyPassword reports locked, none, or set. field is one shadow column.
func classifyPassword(field string) string {
	switch {
	case field == "":
		return "none"
	case strings.HasPrefix(field, "!") || strings.HasPrefix(field, "*"):
		return "locked"
	default:
		return "set"
	}
}

const (
	maxSudoFile  = 256 << 10
	maxSudoRules = 80
	maxSudoCmd   = 180
)

// sudoRules reads /etc/sudoers and /etc/sudoers.d. It does not follow includes
// that point anywhere else, and it does not expand aliases.
func (h *Host) sudoRules() ([]proto.SudoRule, bool) {
	rules := []proto.SudoRule{}
	incomplete := false
	add := func(path string) {
		if len(rules) >= maxSudoRules {
			incomplete = true
			return
		}
		raw, trunc, err := h.readRegular(path, maxSudoFile)
		if err != nil {
			incomplete = true
			return
		}
		if trunc {
			incomplete = true
		}
		got, inc := parseSudoers(string(raw), path)
		incomplete = incomplete || inc
		rules = append(rules, got...)
		if len(rules) > maxSudoRules {
			rules = rules[:maxSudoRules]
			incomplete = true
		}
	}
	add("/etc/sudoers")
	entries, err := os.ReadDir(h.path("/etc/sudoers.d"))
	if err != nil {
		return rules, incomplete
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		// sudo skips names that contain "." or end in "~".
		if name == "" || strings.Contains(name, ".") || strings.HasSuffix(name, "~") {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			incomplete = true
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		add("/etc/sudoers.d/" + name)
	}
	return rules, incomplete
}

// parseSudoers returns command specs. incomplete is set when an include
// points outside /etc/sudoers.d, which this parser does not follow.
func parseSudoers(text, source string) ([]proto.SudoRule, bool) {
	var rules []proto.SudoRule
	incomplete := false
	for _, line := range sudoLogicalLines(text) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if path, ok := sudoInclude(line); ok {
			if path != "/etc/sudoers.d" && path != "/etc/sudoers.d/" {
				incomplete = true
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		token, _, _ := strings.Cut(line, " ")
		if strings.HasPrefix(token, "Defaults") || strings.Contains(token, "_Alias") {
			continue
		}
		rules = append(rules, parseSudoRule(line, source)...)
	}
	return rules, incomplete
}

func sudoLogicalLines(text string) []string {
	var lines []string
	var b strings.Builder
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasSuffix(line, `\`) {
			b.WriteString(strings.TrimSuffix(line, `\`))
			b.WriteByte(' ')
			continue
		}
		b.WriteString(line)
		lines = append(lines, b.String())
		b.Reset()
	}
	if b.Len() > 0 {
		lines = append(lines, b.String())
	}
	return lines
}

// sudoInclude reports the path of a #include, @include, #includedir, or
// @includedir line.
func sudoInclude(line string) (string, bool) {
	for _, p := range []string{"#includedir", "@includedir", "#include", "@include"} {
		if !strings.HasPrefix(line, p) {
			continue
		}
		if len(line) == len(p) {
			return "", true
		}
		if line[len(p)] != ' ' && line[len(p)] != '\t' {
			continue
		}
		return strings.Trim(strings.TrimSpace(line[len(p):]), `"'`), true
	}
	return "", false
}

func parseSudoRule(line, source string) []proto.SudoRule {
	eq := strings.IndexByte(line, '=')
	if eq < 0 {
		return nil
	}
	left := strings.Fields(strings.TrimSpace(line[:eq]))
	if len(left) < 2 {
		return nil
	}
	runAs, rest := lastRunAs(strings.TrimSpace(line[eq+1:]))
	var out []proto.SudoRule
	for _, who := range strings.Split(left[0], ",") {
		who = strings.TrimSpace(who)
		if who == "" {
			continue
		}
		nopass := false
		var cmd string
		for _, spec := range splitSpecs(rest) {
			nopass, cmd = splitSudoTags(spec, nopass)
			cmd = strings.TrimSpace(cmd)
			if cmd == "" {
				continue
			}
			if len(cmd) > maxSudoCmd {
				cmd = cmd[:maxSudoCmd]
			}
			out = append(out, proto.SudoRule{
				Source: source, Who: who, RunAs: runAs, NoPassword: nopass,
				Commands: cmd, Unrestricted: strings.EqualFold(cmd, "ALL"),
			})
		}
	}
	return out
}

// lastRunAs returns the last leading (runas) group and the command spec
// that follows. sudo allows more than one.
func lastRunAs(s string) (runAs, rest string) {
	rest = strings.TrimSpace(s)
	for {
		inner, next, ok := takeRunAs(rest)
		if !ok {
			return runAs, rest
		}
		runAs, rest = inner, strings.TrimSpace(next)
	}
}

func takeRunAs(s string) (inner, rest string, ok bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "(") {
		return "", s, false
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i], s[i+1:], true
			}
		}
	}
	return "", s, false
}

// splitSudoTags reads tags at the start of one command. nopass carries
// from the previous command on the same line, which is how sudo applies it.
func splitSudoTags(spec string, nopass bool) (bool, string) {
	s := strings.TrimSpace(spec)
	tags := []string{
		"NOPASSWD:", "PASSWD:", "NOEXEC:", "EXEC:", "SETENV:", "NOSETENV:",
		"LOG_INPUT:", "NOLOG_INPUT:", "LOG_OUTPUT:", "NOLOG_OUTPUT:", "MAIL:", "NOMAIL:",
	}
	for {
		s = strings.TrimSpace(s)
		up := strings.ToUpper(s)
		tag := ""
		for _, t := range tags {
			if strings.HasPrefix(up, t) {
				tag = t
				break
			}
		}
		if tag == "" {
			break
		}
		switch tag {
		case "NOPASSWD:":
			nopass = true
		case "PASSWD:":
			nopass = false
		}
		s = strings.TrimSpace(s[len(tag):])
	}
	return nopass, s
}

func splitSpecs(s string) []string {
	var out []string
	var b strings.Builder
	quote := byte(0)
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			b.WriteByte(c)
			if c == quote && (i == 0 || s[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
			b.WriteByte(c)
		case '(':
			depth++
			b.WriteByte(c)
		case ')':
			if depth > 0 {
				depth--
			}
			b.WriteByte(c)
		case ',':
			if depth == 0 {
				if spec := strings.TrimSpace(b.String()); spec != "" {
					out = append(out, spec)
				}
				b.Reset()
				continue
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	if spec := strings.TrimSpace(b.String()); spec != "" {
		out = append(out, spec)
	}
	return out
}

// maxAuthorizedKeys caps how much of one authorized_keys file is read.
const maxAuthorizedKeys = 1 << 20

// authorizedKeys reads a user's authorized_keys. Users choose what sits at
// that path, so it must be a regular file (not a link, FIFO, or device),
// is opened without blocking, and only the first MiB is read. gaps names
// what could not be read, so a skipped file or line does not read as clean.
func (h *Host) authorizedKeys(path string) (keys []proto.AuthorizedKey, gaps []string) {
	fi, err := os.Lstat(h.path(path))
	switch {
	case os.IsNotExist(err):
		return nil, nil
	case err != nil:
		return nil, []string{"authorized_keys unreadable"}
	case !fi.Mode().IsRegular():
		return nil, []string{"authorized_keys symlink"}
	}
	f, err := os.OpenFile(h.path(path), os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []string{"authorized_keys symlink"}
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, []string{"authorized_keys symlink"}
	}
	keys, bad := parseAuthorizedKeys(io.LimitReader(f, maxAuthorizedKeys))
	if bad || st.Size() > maxAuthorizedKeys {
		gaps = append(gaps, "authorized_keys unreadable lines")
	}
	return keys, gaps
}

// parseAuthorizedKeys reads key lines. bad is true when a line that is not
// blank or a comment does not parse, or a line is too long to read.
func parseAuthorizedKeys(r io.Reader) (out []proto.AuthorizedKey, bad bool) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 16*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pub, comment, options, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			bad = true
			continue
		}
		from, hasCommand := keyRestrictions(options)
		out = append(out, proto.AuthorizedKey{
			Type: pub.Type(), Fingerprint: ssh.FingerprintSHA256(pub), Comment: comment,
			From: from, HasCommand: hasCommand,
		})
	}
	if sc.Err() != nil {
		bad = true
	}
	return out, bad
}

// keyRestrictions reads from= and whether a command= is set. The command
// text is dropped.
func keyRestrictions(options []string) (from string, hasCommand bool) {
	var froms []string
	for _, o := range options {
		switch {
		case strings.HasPrefix(o, "from="):
			v := strings.Trim(strings.TrimPrefix(o, "from="), `"`)
			if v != "" {
				froms = append(froms, v)
			}
		case strings.HasPrefix(o, "command="):
			hasCommand = true
		}
	}
	from = strings.Join(froms, ", ")
	if len(from) > 120 {
		from = from[:120]
	}
	return from, hasCommand
}

// "root     pts/0        47.129.244.24    2026-10-24T12:24:42+00:00 - ..."
func parseLast(out string) []proto.Login {
	logins := []proto.Login{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 4 || f[0] == "wtmp" || f[0] == "reboot" {
			continue
		}
		var at time.Time
		from := ""
		for i, field := range f[2:] {
			if t, err := time.Parse(time.RFC3339, field); err == nil {
				at = t.UTC()
				if i > 0 {
					from = f[2]
				}
				break
			}
		}
		if at.IsZero() {
			continue
		}
		logins = append(logins, proto.Login{User: f[0], From: from, At: at})
	}
	return logins
}

// ---- Security ------------------------------------------------------------------

var ssProcRe = regexp.MustCompile(`\(\("([^"]+)"(?:,pid=(\d+))?`)

// Security reports listening sockets and protection services.
func (h *Host) Security(ctx context.Context) (proto.SecurityReport, error) {
	rep := proto.SecurityReport{
		Revision: proto.ReportRevision, Listening: []proto.Listener{}, FirewallRules: []string{}, Fail2banJails: []proto.BanJail{},
		Gaps: []string{},
	}
	if out, err := h.output(ctx, 15*time.Second, "ss", "-H", "-tulpn"); err != nil {
		rep.Gaps = append(rep.Gaps, "listening sockets")
	} else {
		names := h.uidNames()
		lookups := 0
		for _, row := range parseSSRows(out) {
			if row.pid > 0 && lookups < 64 {
				row.User = h.userForPID(row.pid, names)
				lookups++
			}
			rep.Listening = append(rep.Listening, row.Listener)
		}
	}
	// Docker publishes are added even when ss shows no socket for them, as
	// with userland-proxy off, where the kernel forwards the port.
	pubs, err := h.publishedPorts(ctx)
	if err != nil {
		rep.Gaps = append(rep.Gaps, "docker ports")
	}
	attachContainers(rep.Listening, pubs)
	rep.Listening = addMissingPublications(rep.Listening, pubs)

	h.readFirewall(ctx, &rep)
	// After ufw: a ufw rule is not protection for a port Docker publishes.
	markDockerUnknown(rep.Listening, pubs)

	rep.CrowdSec = h.serviceState(ctx, "crowdsec.service")
	if rep.CrowdSec == "active" {
		rep.CrowdSecBans = h.crowdSecBans(ctx)
	}
	rep.Fail2ban = h.serviceState(ctx, "fail2ban.service")
	if rep.Fail2ban != "not installed" {
		rep.Fail2banJails = h.fail2banJails(ctx)
	}
	if raw, err := h.readFile("/etc/apt/apt.conf.d/20auto-upgrades"); err == nil {
		rep.UnattendedUpgrades = strings.Contains(strings.ReplaceAll(string(raw), " ", ""), `Unattended-Upgrade"1"`)
	}
	return rep, nil
}

type ssRow struct {
	proto.Listener
	pid int
}

func parseSS(out string) []proto.Listener {
	rows := parseSSRows(out)
	ls := make([]proto.Listener, len(rows))
	for i, row := range rows {
		ls[i] = row.Listener
	}
	return ls
}

func parseSSRows(out string) []ssRow {
	ls := []ssRow{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 5 {
			continue
		}
		local := f[4]
		i := strings.LastIndex(local, ":")
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(local[i+1:])
		if err != nil {
			continue
		}
		addr := local[:i]
		if j := strings.IndexByte(addr, '%'); j >= 0 {
			addr = addr[:j]
		}
		addr = strings.Trim(addr, "[]")
		proc := ""
		pid := 0
		if m := ssProcRe.FindStringSubmatch(l); m != nil {
			proc = m[1]
			pid, _ = strconv.Atoi(m[2])
		}
		ls = append(ls, ssRow{Listener: proto.Listener{
			Proto: f[0], Address: addr, Port: port, Process: proc, Exposure: listenExposure(addr),
		}, pid: pid})
	}
	sort.Slice(ls, func(i, j int) bool {
		if ls[i].Port != ls[j].Port {
			return ls[i].Port < ls[j].Port
		}
		return ls[i].Proto < ls[j].Proto
	})
	return ls
}

// listenExposure classifies the local address. "all" is every interface.
func listenExposure(addr string) string {
	switch addr {
	case "", "*", "0.0.0.0", "::":
		return "all"
	}
	if ip := net.ParseIP(addr); ip != nil && (ip.IsLoopback() || ip.IsUnspecified()) {
		if ip.IsLoopback() {
			return "local"
		}
		return "all"
	}
	return "other"
}

func (h *Host) uidNames() map[int]string {
	raw, err := h.readFile("/etc/passwd")
	if err != nil {
		return nil
	}
	out := map[int]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 3 {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		if err == nil {
			out[uid] = f[0]
		}
	}
	return out
}

func (h *Host) userForPID(pid int, names map[int]string) string {
	raw, _, err := h.readRegular("/proc/"+strconv.Itoa(pid)+"/status", 64<<10)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			return ""
		}
		uid, err := strconv.Atoi(f[1])
		if err != nil {
			return ""
		}
		return names[uid]
	}
	return ""
}

var jailNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

func (h *Host) fail2banJails(ctx context.Context) []proto.BanJail {
	out, err := h.output(ctx, 10*time.Second, "fail2ban-client", "status")
	if err != nil {
		return []proto.BanJail{}
	}
	names := parseFail2banList(out)
	if len(names) > 20 {
		names = names[:20]
	}
	jails := make([]proto.BanJail, 0, len(names))
	for _, name := range names {
		banned := 0
		if st, err := h.output(ctx, 10*time.Second, "fail2ban-client", "status", name); err == nil {
			banned = parseBanned(st)
		}
		jails = append(jails, proto.BanJail{Name: name, Banned: banned})
	}
	return jails
}

func parseFail2banList(out string) []string {
	for _, line := range strings.Split(out, "\n") {
		_, rest, ok := strings.Cut(line, "Jail list:")
		if !ok {
			continue
		}
		var names []string
		for _, n := range strings.Split(rest, ",") {
			n = strings.TrimSpace(n)
			if jailNameRe.MatchString(n) {
				names = append(names, n)
			}
		}
		return names
	}
	return nil
}

func parseBanned(out string) int {
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "Currently banned:") {
			continue
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			return 0
		}
		n, _ := strconv.Atoi(f[len(f)-1])
		return n
	}
	return 0
}

// crowdSecBans counts current decisions. Addresses in the command output
// are not kept.
func (h *Host) crowdSecBans(ctx context.Context) int {
	out, err := h.output(ctx, 15*time.Second, "cscli", "decisions", "list", "-o", "json")
	if err != nil {
		return 0
	}
	return countDecisions(out)
}

func countDecisions(out string) int {
	if len(out) > 256<<10 {
		out = out[:256<<10]
	}
	return strings.Count(out, `"origin"`)
}

// readRegular reads a regular file and refuses symlinks, FIFOs, and devices.
// truncated is true when the file is longer than max.
func (h *Host) readRegular(path string, max int64) ([]byte, bool, error) {
	full := h.path(path)
	fi, err := os.Lstat(full)
	if err != nil {
		return nil, false, err
	}
	if !fi.Mode().IsRegular() {
		return nil, false, fmt.Errorf("not a regular file")
	}
	f, err := os.OpenFile(full, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return nil, false, fmt.Errorf("not a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, max))
	return b, st.Size() > max, err
}

func (h *Host) serviceState(ctx context.Context, unit string) string {
	out, err := h.output(ctx, 10*time.Second, "systemctl", "list-unit-files", unit, "--no-legend", "--no-pager")
	if err != nil || strings.TrimSpace(out) == "" {
		return "not installed"
	}
	res, _ := h.Run.Run(ctx, 10*time.Second, "systemctl", "is-active", unit)
	return strings.TrimSpace(string(res.Stdout))
}

// ---- Scheduled jobs -------------------------------------------------------------

// Jobs lists cron entries and systemd timers.
func (h *Host) Jobs(ctx context.Context) (proto.JobsReport, error) {
	rep := proto.JobsReport{Cron: []proto.CronEntry{}, Timers: []proto.Timer{}}
	rep.Cron = append(rep.Cron, h.cronFile("/etc/crontab", "", true)...)
	if entries, err := os.ReadDir(h.path("/etc/cron.d")); err == nil {
		for _, e := range entries {
			if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				rep.Cron = append(rep.Cron, h.cronFile("/etc/cron.d/"+e.Name(), "", true)...)
			}
		}
	}
	for _, dir := range []string{"/var/spool/cron/crontabs", "/var/spool/cron"} {
		entries, err := os.ReadDir(h.path(dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				rep.Cron = append(rep.Cron, h.cronFile(dir+"/"+e.Name(), e.Name(), false)...)
			}
		}
	}

	out, err := h.output(ctx, 15*time.Second, "systemctl", "list-timers", "--all", "--output=json", "--no-pager")
	if err != nil {
		return rep, nil
	}
	var timers []struct {
		Unit      string `json:"unit"`
		Activates string `json:"activates"`
		Next      int64  `json:"next"`
		Last      int64  `json:"last"`
	}
	if json.Unmarshal([]byte(out), &timers) != nil {
		return rep, nil
	}
	var services []string
	for _, t := range timers {
		services = append(services, t.Activates)
	}
	props := h.unitProps(ctx, services, "Id", "Result", "ActiveState")
	for _, t := range timers {
		p := props[t.Activates]
		tm := proto.Timer{Unit: t.Unit, Activates: t.Activates, Result: p["Result"], Failed: p["ActiveState"] == "failed"}
		if t.Next > 0 {
			tm.Next = time.UnixMicro(t.Next).UTC()
		}
		if t.Last > 0 {
			tm.Last = time.UnixMicro(t.Last).UTC()
		}
		rep.Timers = append(rep.Timers, tm)
	}
	return rep, nil
}

// cronFile parses a crontab. System crontabs have a user column.
func (h *Host) cronFile(path, user string, system bool) []proto.CronEntry {
	raw, err := h.readFile(path)
	if err != nil {
		return nil
	}
	var out []proto.CronEntry
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) > 0 && strings.Contains(f[0], "=") {
			continue // environment assignment
		}
		var schedule string
		var rest []string
		if strings.HasPrefix(f[0], "@") {
			schedule, rest = f[0], f[1:]
		} else if len(f) >= 6 {
			schedule, rest = strings.Join(f[:5], " "), f[5:]
		} else {
			continue
		}
		u := user
		if system {
			if len(rest) < 2 {
				continue
			}
			u, rest = rest[0], rest[1:]
		}
		out = append(out, proto.CronEntry{Source: path, User: u, Schedule: schedule, Command: strings.Join(rest, " ")})
	}
	return out
}
