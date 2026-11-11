package hostops

import (
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// Captured from Ubuntu 24.04 on arm64.
const simOutput = `Reading package lists...
Inst motd-news-config [13ubuntu10.4] (13ubuntu10.5 Ubuntu:24.04/noble-updates [all])
Inst libc6 [2.39-0ubuntu8.8] (2.39-0ubuntu8.9 Ubuntu:24.04/noble-updates, Ubuntu:24.04/noble-security [arm64])
Inst console-setup-linux [1.226ubuntu1] (1.226ubuntu1.1 Ubuntu:24.04/noble-updates [all]) []
Conf motd-news-config (13ubuntu10.5 Ubuntu:24.04/noble-updates [all])`

func TestParseSimulation(t *testing.T) {
	pkgs := parseSimulation(simOutput)
	if len(pkgs) != 3 {
		t.Fatalf("want 3 packages, got %+v", pkgs)
	}
	byName := map[string]bool{}
	for _, p := range pkgs {
		byName[p.Name] = p.Security
	}
	if !byName["libc6"] || byName["motd-news-config"] {
		t.Fatalf("security flags wrong: %+v", pkgs)
	}
}

const policyOutput = `apparmor:
  Installed: 4.0.1really4.0.1-0ubuntu0.24.04.6
  Candidate: 4.0.1really4.0.1-0ubuntu0.24.04.9
  Version table:
     4.0.1really4.0.1-0ubuntu0.24.04.9 500
        500 http://ports.ubuntu.com/ubuntu-ports noble-updates/main arm64 Packages
        500 http://ports.ubuntu.com/ubuntu-ports noble-security/main arm64 Packages
 *** 4.0.1really4.0.1-0ubuntu0.24.04.6 100
        100 /var/lib/dpkg/status
cloud-init:
  Installed: 25.3-0ubuntu1~24.04.1
  Candidate: 26.2-0ubuntu1~24.04.2
  Version table:
     26.2-0ubuntu1~24.04.2 500
        500 http://ports.ubuntu.com/ubuntu-ports noble-updates/main arm64 Packages
 *** 25.3-0ubuntu1~24.04.1 100
        100 /var/lib/dpkg/status
     24.1.3-0ubuntu3 500
        500 http://ports.ubuntu.com/ubuntu-ports noble-security/main arm64 Packages
`

func TestSecurityCandidates(t *testing.T) {
	got := securityCandidates(policyOutput)
	if !got["apparmor"] {
		t.Error("apparmor's candidate is in noble-security")
	}
	if got["cloud-init"] {
		t.Error("cloud-init's candidate is only in noble-updates; an older version in -security must not count")
	}
}

func TestParseLast(t *testing.T) {
	out := "root     pts/0        47.129.244.24    2026-10-24T12:24:42+00:00 - 2026-10-24T12:24:45+00:00  (00:00)\nreboot   system boot  6.17.0           2026-07-12T14:30:27+00:00   still running\n\nwtmp begins 2026-07-12T14:30:31+00:00"
	ls := parseLast(out)
	if len(ls) != 1 || ls[0].User != "root" || ls[0].From != "47.129.244.24" || !ls[0].At.Equal(time.Date(2026, 10, 24, 12, 24, 42, 0, time.UTC)) {
		t.Fatalf("got %+v", ls)
	}
}

func TestParseSS(t *testing.T) {
	out := `tcp LISTEN 0 4096 0.0.0.0:443 0.0.0.0:* users:(("docker-proxy",pid=1,fd=7))
tcp LISTEN 0 128 [::]:22 [::]:* users:(("sshd",pid=2,fd=4))
udp UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=3,fd=14))`
	ls := parseSS(out)
	if len(ls) != 3 || ls[0].Port != 22 || ls[0].Address != "::" || ls[0].Process != "sshd" || ls[0].Exposure != "all" {
		t.Fatalf("got %+v", ls)
	}
	if ls[1].Address != "127.0.0.53" || ls[1].Exposure != "local" || ls[2].Exposure != "all" || ls[2].Process != "docker-proxy" {
		t.Fatalf("got %+v", ls)
	}
	attachContainers(ls, map[string]string{"443/tcp": "proxy"})
	if ls[2].Container != "proxy" || ls[0].Container != "" {
		t.Fatalf("container join: %+v", ls)
	}
}

func TestClassifyPassword(t *testing.T) {
	hash := "$y$j9T$salt$hash"
	got := map[string]string{
		"": "none", "*": "locked", "!": "locked", "!!": "locked", "!*": "locked",
		"!" + hash: "locked", hash: "set",
	}
	for field, want := range got {
		if classifyPassword(field) != want {
			t.Errorf("classifyPassword(%q) = %s, want %s", field, classifyPassword(field), want)
		}
	}
	if strings.Contains(classifyPassword(hash), "salt") || strings.Contains(classifyPassword(hash), "$") {
		t.Fatal("password state must not contain the hash")
	}
}

func TestParseSudoers(t *testing.T) {
	text := `
Defaults env_reset
root ALL=(ALL:ALL) ALL
%sudo ALL=(ALL:ALL) ALL
deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart nginx, /usr/bin/systemctl reload nginx
esa ALL=(root) NOPASSWD: /usr/local/lib/esa/esa-helper *
limited ALL=(root) NOPASSWD: /bin/a, PASSWD: /bin/b
#includedir /etc/sudoers.d
@include /tmp/not-followed
`
	rules, incomplete := parseSudoers(text, "/etc/sudoers")
	if !incomplete {
		t.Fatal("an include outside /etc/sudoers.d must be flagged and not followed")
	}
	byWho := map[string]proto.SudoRule{}
	for _, r := range rules {
		byWho[r.Who+" "+r.Commands] = r
	}
	root := byWho["root ALL"]
	if !root.Unrestricted || root.NoPassword || root.RunAs != "ALL:ALL" {
		t.Fatalf("root: %+v", root)
	}
	sudo := byWho["%sudo ALL"]
	if !sudo.Unrestricted || sudo.Who != "%sudo" {
		t.Fatalf("sudo group: %+v", sudo)
	}
	esa := byWho["esa /usr/local/lib/esa/esa-helper *"]
	if esa.Unrestricted || !esa.NoPassword || esa.RunAs != "root" {
		t.Fatalf("esa: %+v", esa)
	}
	if _, ok := byWho["deploy /usr/bin/systemctl restart nginx"]; !ok {
		t.Fatalf("missing deploy restart: %+v", rules)
	}
	reload := byWho["deploy /usr/bin/systemctl reload nginx"]
	if !reload.NoPassword {
		t.Fatalf("deploy reload: %+v", reload)
	}
	if byWho["limited /bin/a"].NoPassword != true || byWho["limited /bin/b"].NoPassword {
		t.Fatalf("PASSWD must clear NOPASSWD: %+v", rules)
	}
}

func TestUFWRules(t *testing.T) {
	out := "Status: active\n\nTo                         Action      From\n--                         ------      ----\n22/tcp                     ALLOW       Anywhere\n80/tcp                     ALLOW       Anywhere\n"
	rules := ufwRules(out)
	if len(rules) != 2 || !strings.HasPrefix(rules[0], "22/tcp") {
		t.Fatalf("got %#v", rules)
	}
	if len(ufwRules("Status: inactive")) != 0 {
		t.Fatal("inactive status has no rules")
	}
}

func TestDockerPublishIsNotUFWProof(t *testing.T) {
	published := map[string]string{"80/tcp": "web"}
	ls := []proto.Listener{{Proto: "tcp", Port: 80, Exposure: "all", UFW: "allow", Process: "docker-proxy"}}
	markDockerUnknown(ls, published)
	if ls[0].UFW != "unknown" {
		t.Fatalf("ufw on a published port: %s", ls[0].UFW)
	}
	added := addMissingPublications(nil, published)
	if len(added) != 1 || added[0].Process != "docker" || added[0].UFW != "unknown" || added[0].Container != "web" {
		t.Fatalf("missing publication: %+v", added)
	}
	if got := addMissingPublications(ls, published); len(got) != 1 {
		t.Fatalf("a socket already listed must not be added again: %+v", got)
	}
}

func TestUFWFor(t *testing.T) {
	rules := []string{
		"22/tcp                     ALLOW       Anywhere",
		"22/tcp (v6)                ALLOW       Anywhere (v6)",
		"8080/tcp                   DENY        Anywhere",
		"25/tcp                     DENY        Anywhere",
		"25/tcp                     ALLOW       10.1.0.0/24",
	}
	if ufwFor(22, "tcp", rules) != "allow" || ufwFor(8080, "tcp", rules) != "deny" || ufwFor(443, "tcp", rules) != "unmentioned" {
		t.Fatalf("22=%s 8080=%s 443=%s", ufwFor(22, "tcp", rules), ufwFor(8080, "tcp", rules), ufwFor(443, "tcp", rules))
	}
	if ufwFor(25, "tcp", rules) != "allow" {
		t.Fatal("an allow must win over a deny for the same port")
	}
}

func TestKeyRestrictions(t *testing.T) {
	from, cmd := keyRestrictions([]string{`from="10.0.0.1,10.0.0.2"`, `command="/usr/bin/rrsync secret-token"`})
	if from != "10.0.0.1,10.0.0.2" || !cmd {
		t.Fatalf("got %q %v", from, cmd)
	}
	if strings.Contains(from, "secret") {
		t.Fatal("from must not contain the forced command")
	}
}

func TestFail2banAndDecisions(t *testing.T) {
	list := "Status\n|- Number of jail:\t2\n`- Jail list:\tsshd, recidive\n"
	names := parseFail2banList(list)
	if len(names) != 2 || names[0] != "sshd" || names[1] != "recidive" {
		t.Fatalf("jails: %#v", names)
	}
	if parseFail2banList("Jail list: bad name, ../x") != nil && len(parseFail2banList("Jail list: bad name, ../x")) != 0 {
		t.Fatal("jail names with spaces or dots-dot must be dropped")
	}
	banned := parseBanned("|- Currently banned:\t4\n")
	if banned != 4 {
		t.Fatalf("banned %d", banned)
	}
	raw := `[{"origin":"cscli","value":"203.0.113.5"},{"origin":"crowdsec","value":"198.51.100.9"}]`
	if n := countDecisions(raw); n != 2 {
		t.Fatalf("decisions %d", n)
	}
}

func TestJournalSize(t *testing.T) {
	for s, ok := range map[string]bool{"200M": true, "1G": true, "49M": false, "10K": false, "200": false, "1T": false, "200M;rm": false} {
		if got := validJournalSize(s); got != ok {
			t.Errorf("validJournalSize(%q) = %v", s, got)
		}
	}
}

func TestDockerLogFrames(t *testing.T) {
	frame := func(stream byte, s string) []byte {
		b := []byte{stream, 0, 0, 0, 0, 0, 0, byte(len(s))}
		return append(b, s...)
	}
	body := append(frame(1, "2026-10-27T04:07:02.463933619Z hello\n"), frame(2, "2026-10-27T04:07:03Z oops\n")...)
	lines := parseDockerLogs(body, "application/vnd.docker.multiplexed-stream")
	if len(lines) != 2 || lines[0].Message != "hello" || lines[1].Stream != "stderr" {
		t.Fatalf("got %+v", lines)
	}
}
