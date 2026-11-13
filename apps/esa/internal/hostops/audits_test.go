package hostops

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

func TestParseSSZoneBeforeBrackets(t *testing.T) {
	out := `tcp LISTEN 0 128 [::]%eth0:22 [::]:* users:(("sshd",pid=2,fd=4))
tcp LISTEN 0 128 *%eth0:80 *:*
tcp LISTEN 0 128 0.0.0.0%lo:81 0.0.0.0:*
tcp LISTEN 0 128 [fe80::1]%eth0:82 [::]:*`
	ls := parseSS(out)
	want := map[int][2]string{22: {"::", "all"}, 80: {"*", "all"}, 81: {"0.0.0.0", "all"}, 82: {"fe80::1", "other"}}
	if len(ls) != len(want) {
		t.Fatalf("got %+v", ls)
	}
	for _, l := range ls {
		w := want[l.Port]
		if l.Address != w[0] || l.Exposure != w[1] {
			t.Errorf("port %d: address %q exposure %q, want %q %q", l.Port, l.Address, l.Exposure, w[0], w[1])
		}
	}
}

func testKeyLine(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))) + " user@host"
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizedKeysGaps(t *testing.T) {
	root := t.TempDir()
	h := &Host{Root: root}
	key := testKeyLine(t)

	writeFile(t, root+"/home/good/.ssh/authorized_keys", "# comment\n"+key+"\n")
	keys, gaps := h.authorizedKeys("/home/good/.ssh/authorized_keys")
	if len(keys) != 1 || len(gaps) != 0 {
		t.Fatalf("good: %+v %v", keys, gaps)
	}

	writeFile(t, root+"/home/bad/.ssh/authorized_keys", key+"\nnot a key line\n")
	keys, gaps = h.authorizedKeys("/home/bad/.ssh/authorized_keys")
	if len(keys) != 1 || !slices.Equal(gaps, []string{"authorized_keys unreadable lines"}) {
		t.Fatalf("bad line: %+v %v", keys, gaps)
	}

	writeFile(t, root+"/elsewhere", key+"\n")
	if err := os.MkdirAll(root+"/home/link/.ssh", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root+"/elsewhere", root+"/home/link/.ssh/authorized_keys"); err != nil {
		t.Fatal(err)
	}
	keys, gaps = h.authorizedKeys("/home/link/.ssh/authorized_keys")
	if len(keys) != 0 || !slices.Equal(gaps, []string{"authorized_keys symlink"}) {
		t.Fatalf("symlink must not be followed: %+v %v", keys, gaps)
	}

	keys, gaps = h.authorizedKeys("/home/none/.ssh/authorized_keys")
	if len(keys) != 0 || len(gaps) != 0 {
		t.Fatalf("a missing file is not a gap: %+v %v", keys, gaps)
	}
}

func TestDefaultAuthorizedKeysFile(t *testing.T) {
	for v, want := range map[string]bool{
		".ssh/authorized_keys .ssh/authorized_keys2": true,
		".ssh/authorized_keys":                       true,
		"%h/.ssh/authorized_keys":                    true,
		"/etc/ssh/keys/%u":                           false,
		".ssh/authorized_keys /etc/ssh/keys/%u":      false,
		"none":                                       false,
	} {
		if got := defaultAuthorizedKeysFile(v); got != want {
			t.Errorf("defaultAuthorizedKeysFile(%q) = %v", v, got)
		}
	}
}

func TestHasMatchLine(t *testing.T) {
	if !hasMatchLine("Port 22\nMatch User deploy\n  PasswordAuthentication yes\n") {
		t.Fatal("Match line not found")
	}
	if !hasMatchLine("  match=Address 10.0.0.0/8\n") {
		t.Fatal("lower case and = must count")
	}
	if hasMatchLine("# Match User x\nPort 22\nMatchFoo x\n") {
		t.Fatal("a comment or another keyword is not a Match block")
	}
}

func accessHost(t *testing.T, sshdT string) (*Host, string) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root+"/etc/passwd", "root:x:0:0:root:/root:/bin/bash\n")
	return &Host{Root: root, Run: &recordingRunner{out: map[string]string{"sshd -T": sshdT}}}, root
}

func TestAccessSSHGaps(t *testing.T) {
	h, root := accessHost(t, "port 22\npasswordauthentication no\nauthorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2\n")
	writeFile(t, root+"/etc/ssh/sshd_config", "Include /etc/ssh/sshd_config.d/*.conf\nPort 22\n")
	writeFile(t, root+"/etc/ssh/sshd_config.d/50-cloud.conf", "PasswordAuthentication no\n")
	rep, err := h.Access(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(rep.Gaps, "ssh Match blocks") || slices.Contains(rep.Gaps, "custom AuthorizedKeysFile") {
		t.Fatalf("clean config has gaps: %v", rep.Gaps)
	}

	writeFile(t, root+"/etc/ssh/sshd_config.d/60-deploy.conf", "Match User deploy\n  PasswordAuthentication yes\n")
	h.Run = &recordingRunner{out: map[string]string{"sshd -T": "port 22\nauthorizedkeysfile /etc/ssh/keys/%u\n"}}
	rep, err = h.Access(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(rep.Gaps, "ssh Match blocks") || !slices.Contains(rep.Gaps, "custom AuthorizedKeysFile") {
		t.Fatalf("gaps: %v", rep.Gaps)
	}
}

const ufwVerbose = `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
OpenSSH                    ALLOW IN    Anywhere
Nginx Full                 ALLOW IN    Anywhere
6000:6007/tcp              ALLOW IN    Anywhere
80,443/udp                 ALLOW IN    Anywhere
10.0.0.5 9000/tcp          DENY IN     Anywhere
5432/tcp on eth1           ALLOW IN    10.0.0.0/8
Anywhere                   ALLOW IN    192.168.1.10
25/tcp                     ALLOW OUT   Anywhere (out)
OpenSSH (v6)               ALLOW IN    Anywhere (v6)
`

func TestUFWVerbose(t *testing.T) {
	profiles := parseUFWProfiles("[OpenSSH]\ntitle=Secure shell\nports=22/tcp\n\n[Nginx Full]\nports=80,443/tcp\n\n[Bad]\nports=abc\n")
	if _, ok := profiles["Bad"]; ok || len(profiles) != 2 {
		t.Fatalf("profiles: %+v", profiles)
	}
	if got := ufwDefaultIncoming(ufwVerbose); got != "deny" {
		t.Fatalf("default incoming %q", got)
	}
	if got := ufwDefaultIncoming("Default: allow (incoming), allow (outgoing)"); got != "allow" {
		t.Fatalf("default allow %q", got)
	}
	lines := ufwRuleLines(ufwVerbose, maxUFWRules)
	rules, unresolved := parseUFW(lines, profiles)
	if unresolved {
		t.Fatal("known profiles must resolve")
	}
	for _, c := range []struct {
		port  int
		proto string
		want  string
	}{
		{22, "tcp", "allow"}, {443, "tcp", "allow"}, {6003, "tcp", "allow"}, {6008, "tcp", "allow"},
		{443, "udp", "allow"}, {9000, "tcp", "allow"}, {5432, "tcp", "allow"},
	} {
		if got := ufwFor(c.port, c.proto, rules); got != c.want {
			t.Errorf("%d/%s = %s, want %s", c.port, c.proto, got, c.want)
		}
	}

	// Without the all-ports rule, ranges and profiles still count, an OUT
	// rule does not, and a deny is seen.
	var narrow []string
	for _, l := range lines {
		if !strings.HasPrefix(l, "Anywhere") {
			narrow = append(narrow, l)
		}
	}
	rules, _ = parseUFW(narrow, profiles)
	for _, c := range []struct {
		port int
		want string
	}{{6000, "allow"}, {6007, "allow"}, {6008, "unmentioned"}, {25, "unmentioned"}, {9000, "deny"}, {22, "allow"}, {80, "allow"}} {
		if got := ufwFor(c.port, "tcp", rules); got != c.want {
			t.Errorf("narrow %d/tcp = %s, want %s", c.port, got, c.want)
		}
	}

	if _, unresolved := parseUFW(lines, nil); !unresolved {
		t.Fatal("an app profile that is not known must be reported")
	}
}

func TestSecurityUFWDefaultAndProfiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root+"/etc/ufw/applications.d/openssh-server", "[OpenSSH]\nports=22/tcp\n")
	ss := `tcp LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=2,fd=4))
tcp LISTEN 0 128 0.0.0.0:3000 0.0.0.0:* users:(("node",pid=3,fd=4))`
	h := &Host{Root: root, Run: &recordingRunner{out: map[string]string{
		"ss ":                ss,
		"ufw status verbose": strings.Replace(ufwVerbose, "Nginx Full", "Unknown App", 1),
	}}}
	rep, err := h.Security(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Firewall != "ufw active" || rep.FirewallDefaultIncoming != "deny" {
		t.Fatalf("firewall %q default %q", rep.Firewall, rep.FirewallDefaultIncoming)
	}
	if !slices.Contains(rep.Gaps, "ufw app profiles") {
		t.Fatalf("gaps: %v", rep.Gaps)
	}
	for _, l := range rep.Listening {
		if l.Port == 22 && l.UFW != "allow" {
			t.Fatalf("OpenSSH profile must cover 22: %+v", l)
		}
	}

	writeFile(t, root+"/etc/default/ufw", "IPV6=yes\nDEFAULT_INPUT_POLICY=\"ACCEPT\"\n")
	h.Run = &recordingRunner{out: map[string]string{"ufw status verbose": "Status: active\n\nTo Action From\n-- ------ ----\n"}}
	rep, _ = h.Security(context.Background())
	if rep.FirewallDefaultIncoming != "allow" {
		t.Fatalf("default from file: %q", rep.FirewallDefaultIncoming)
	}
}

func TestSecurityDockerError(t *testing.T) {
	h := &Host{Root: t.TempDir(), Run: &recordingRunner{}, Docker: NewDockerClient(filepath.Join(t.TempDir(), "none.sock"))}
	rep, err := h.Security(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(rep.Gaps, "docker ports") {
		t.Fatalf("a Docker error must be a gap: %v", rep.Gaps)
	}
}

func dockerWithPorts(t *testing.T, containers string) *DockerClient {
	t.Helper()
	dir, err := os.MkdirTemp("", "esa-docker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(containers))
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return NewDockerClient(sock)
}

const dockerPorts = `[
 {"Id":"aaaaaaaaaaaa1111","Names":["/web"],"State":"running","Ports":[
   {"IP":"0.0.0.0","PrivatePort":80,"PublicPort":8080,"Type":"tcp"},
   {"IP":"::","PrivatePort":80,"PublicPort":8080,"Type":"tcp"},
   {"IP":"127.0.0.1","PrivatePort":9000,"PublicPort":9000,"Type":"tcp"},
   {"PrivatePort":5000,"Type":"tcp"}]},
 {"Id":"bbbbbbbbbbbb2222","Names":["/api"],"State":"running","Ports":[
   {"IP":"0.0.0.0","PrivatePort":3000,"PublicPort":3000,"Type":"tcp"}]},
 {"Id":"cccccccccccc3333","Names":["/old"],"State":"exited","Ports":[
   {"IP":"0.0.0.0","PrivatePort":1,"PublicPort":7000,"Type":"tcp"}]}
]`

func TestPublicationsFrom(t *testing.T) {
	var list []dockerContainer
	if err := json.Unmarshal([]byte(dockerPorts), &list); err != nil {
		t.Fatal(err)
	}
	pubs := publicationsFrom(list)
	if len(pubs) != 4 {
		t.Fatalf("pubs: %+v", pubs)
	}
	for _, p := range pubs {
		if p.Port == 7000 || p.Port == 5000 {
			t.Fatalf("a stopped or unpublished port was listed: %+v", p)
		}
	}
}

func TestSecurityDockerPublishWithoutProxy(t *testing.T) {
	// userland-proxy off: ss shows sshd and api's docker-proxy, nothing for web.
	ss := `tcp LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=2,fd=4))
tcp LISTEN 0 128 0.0.0.0:3000 0.0.0.0:* users:(("docker-proxy",pid=3,fd=4))`
	h := &Host{Root: t.TempDir(), Docker: dockerWithPorts(t, dockerPorts), Run: &recordingRunner{out: map[string]string{
		"ss ":                ss,
		"ufw status verbose": "Status: active\nDefault: deny (incoming)\n\nTo Action From\n-- ------ ----\n22/tcp ALLOW IN Anywhere\n3000/tcp ALLOW IN Anywhere\n8080/tcp ALLOW IN Anywhere\n",
	}}}
	rep, err := h.Security(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byPort := map[int][]proto.Listener{}
	for _, l := range rep.Listening {
		byPort[l.Port] = append(byPort[l.Port], l)
	}
	if len(byPort[3000]) != 1 || byPort[3000][0].Container != "api" || byPort[3000][0].UFW != "unknown" {
		t.Fatalf("3000 must not be duplicated and ufw must not cover it: %+v", byPort[3000])
	}
	if len(byPort[8080]) != 1 || byPort[8080][0].Exposure != "all" || byPort[8080][0].UFW != "unknown" || byPort[8080][0].Container != "web" {
		t.Fatalf("8080 published without a proxy: %+v", byPort[8080])
	}
	if len(byPort[9000]) != 1 || byPort[9000][0].Exposure != "local" {
		t.Fatalf("9000 is bound to loopback: %+v", byPort[9000])
	}
	if byPort[22][0].UFW != "allow" {
		t.Fatalf("22: %+v", byPort[22])
	}
}

type failRunner struct{}

func (failRunner) Run(context.Context, time.Duration, string, ...string) (Result, error) {
	return Result{Exit: 2, Stderr: []byte("dpkg-query: error")}, nil
}

func TestInventoryInstalledOnly(t *testing.T) {
	out := "installed\tbash\t5.2-1\tamd64\nconfig-files\toldpkg\t1.0\tamd64\nhalf-installed\tbroken\t2.0\tamd64\ninstalled\tlibc6\t2.39\tarm64\n"
	pkgs, truncated := parseDpkg(out)
	if truncated || len(pkgs) != 2 || pkgs[0].Name != "bash" || pkgs[1].Name != "libc6" {
		t.Fatalf("pkgs: %+v", pkgs)
	}

	h := &Host{Root: t.TempDir(), Run: failRunner{}}
	if _, err := h.Inventory(context.Background()); err == nil {
		t.Fatal("a failed dpkg-query must be an error")
	}
	h.Run = &recordingRunner{}
	if _, err := h.Inventory(context.Background()); err == nil {
		t.Fatal("an empty package list must not be a success")
	}
	h.Run = &recordingRunner{out: map[string]string{"dpkg-query": out}}
	inv, err := h.Inventory(context.Background())
	if err != nil || len(inv.Packages) != 2 {
		t.Fatalf("inventory: %+v %v", inv, err)
	}
}

func TestRecoveryFileRefusesLinksAndFIFOs(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "real.json")
	writeFile(t, target, `{"schema":1,"tool":"restic","dataset":"db"}`)
	if err := os.Symlink(target, filepath.Join(dir, "restic.json")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "borg.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan proto.RecoveryReport, 1)
	go func() { done <- readRecovery(dir) }()
	select {
	case rep := <-done:
		if len(rep.Sources) != 0 || !slices.Equal(rep.Gaps, []string{"restic.json: unreadable", "borg.json: unreadable"}) {
			t.Fatalf("report: %+v", rep)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO must not block")
	}

	big := t.TempDir()
	writeFile(t, filepath.Join(big, "restic.json"), strings.Repeat(" ", recoveryMaxByte+1))
	if _, gap := readRecoveryFile(filepath.Join(big, "restic.json")); gap != "unreadable" {
		t.Fatalf("oversize: %q", gap)
	}
}
