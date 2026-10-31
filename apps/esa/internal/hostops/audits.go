package hostops

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
	rep := proto.AccessReport{Users: []proto.LocalUser{}, Groups: map[string][]string{}, Logins: []proto.Login{}}

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
		u := proto.LocalUser{Name: f[0], UID: uid, Home: f[5], Shell: f[6], AuthorizedKeys: []proto.AuthorizedKey{}}
		for _, name := range []string{"authorized_keys", "authorized_keys2"} {
			u.AuthorizedKeys = append(u.AuthorizedKeys, h.authorizedKeys(filepath.Join(f[5], ".ssh", name))...)
		}
		rep.Users = append(rep.Users, u)
	}

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
	if out, err := h.output(ctx, 10*time.Second, "sshd", "-T"); err == nil {
		for _, l := range strings.Split(out, "\n") {
			k, v, _ := strings.Cut(l, " ")
			switch k {
			case "passwordauthentication":
				rep.PasswordAuth = v == "yes"
			case "permitrootlogin":
				rep.PermitRootLogin = v
			}
		}
	}
	return rep, nil
}

func (h *Host) authorizedKeys(path string) []proto.AuthorizedKey {
	f, err := os.Open(h.path(path))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []proto.AuthorizedKey
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 16*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pub, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			continue
		}
		out = append(out, proto.AuthorizedKey{Type: pub.Type(), Fingerprint: ssh.FingerprintSHA256(pub), Comment: comment})
	}
	return out
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

var ssProcRe = regexp.MustCompile(`\(\("([^"]+)"`)

// Security reports listening sockets and protection services.
func (h *Host) Security(ctx context.Context) (proto.SecurityReport, error) {
	rep := proto.SecurityReport{Listening: []proto.Listener{}}
	if out, err := h.output(ctx, 15*time.Second, "ss", "-H", "-tulpn"); err == nil {
		rep.Listening = parseSS(out)
	}

	rep.Firewall = "none"
	if out, err := h.output(ctx, 10*time.Second, "ufw", "status"); err == nil && strings.Contains(out, "Status: active") {
		rep.Firewall = "ufw active"
	} else if out, err := h.output(ctx, 10*time.Second, "nft", "list", "ruleset"); err == nil && strings.Contains(out, "chain") {
		rep.Firewall = "nftables rules"
	}

	rep.CrowdSec = h.serviceState(ctx, "crowdsec.service")
	rep.Fail2ban = h.serviceState(ctx, "fail2ban.service")
	if raw, err := h.readFile("/etc/apt/apt.conf.d/20auto-upgrades"); err == nil {
		rep.UnattendedUpgrades = strings.Contains(strings.ReplaceAll(string(raw), " ", ""), `Unattended-Upgrade"1"`)
	}
	return rep, nil
}

func parseSS(out string) []proto.Listener {
	ls := []proto.Listener{}
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
		addr := strings.Trim(local[:i], "[]")
		if j := strings.IndexByte(addr, '%'); j >= 0 {
			addr = addr[:j]
		}
		proc := ""
		if m := ssProcRe.FindStringSubmatch(l); m != nil {
			proc = m[1]
		}
		ls = append(ls, proto.Listener{Proto: f[0], Address: addr, Port: port, Process: proc})
	}
	sort.Slice(ls, func(i, j int) bool {
		if ls[i].Port != ls[j].Port {
			return ls[i].Port < ls[j].Port
		}
		return ls[i].Proto < ls[j].Proto
	})
	return ls
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
