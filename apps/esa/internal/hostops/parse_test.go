package hostops

import (
	"testing"
	"time"
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
	if len(ls) != 3 || ls[0].Port != 22 || ls[0].Address != "::" || ls[0].Process != "sshd" || ls[1].Address != "127.0.0.53" {
		t.Fatalf("got %+v", ls)
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
