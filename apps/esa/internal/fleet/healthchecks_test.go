package fleet

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestValidChecks(t *testing.T) {
	got, err := validChecks([]HealthCheck{{
		Name: "web", Kind: "http", Blocking: true, Port: 443, TLS: true, Path: "/health",
	}})
	if err != nil || got[0].Method != "GET" {
		t.Fatalf("http: %+v %v", got, err)
	}
	if _, err := validChecks([]HealthCheck{{Name: "web", Kind: "http", Path: "http://169.254.169.254/"}}); err == nil {
		t.Fatal("a check must not take a host")
	}
	if _, err := validChecks([]HealthCheck{{Name: "ssh", Kind: "tcp", Port: 22}, {Name: "ssh", Kind: "tcp", Port: 22}}); err == nil {
		t.Fatal("duplicate name")
	}
	unit, err := validChecks([]HealthCheck{{Name: "nginx", Kind: "unit", Unit: "nginx"}})
	if err != nil || unit[0].Unit != "nginx.service" {
		t.Fatalf("unit: %+v %v", unit, err)
	}
}

func TestPartitionCanaries(t *testing.T) {
	items := []store.RunServer{{ServerID: 1}, {ServerID: 2}, {ServerID: 3}}
	canary, rest := partitionCanaries(items, []int{0, 1, 2}, []int64{3, 1})
	if len(canary) != 2 || canary[0] != 2 || canary[1] != 0 || len(rest) != 1 || rest[0] != 1 {
		t.Fatalf("canary %v rest %v", canary, rest)
	}
}

func TestCheckHTTPDoesNotFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(w, r, "http://169.254.169.254/latest", http.StatusFound)
	}))
	defer srv.Close()
	host, port := splitTestHost(t, srv.URL)
	s := &Service{allowPrivate: true}
	if got := s.checkHTTP(context.Background(), host, HealthCheck{Method: http.MethodGet, Path: "/go", Port: port}); !strings.Contains(got, "redirect") {
		t.Fatalf("redirect: %q", got)
	}
	if got := s.checkHTTP(context.Background(), host, HealthCheck{Method: http.MethodGet, Path: "/health", Port: port}); got != "" {
		t.Fatalf("pass: %q", got)
	}
}

func TestSampleUntilStopsOnFailure(t *testing.T) {
	at := time.Unix(0, 0)
	n, reason := sampleUntil(30*time.Second, 15*time.Second, func() time.Time { return at }, func(d time.Duration) { at = at.Add(d) }, func() string {
		if at.Before(time.Unix(15, 0)) {
			return ""
		}
		return "web: connection refused"
	})
	if n != 2 || reason != "web: connection refused" {
		t.Fatalf("samples %d reason %q", n, reason)
	}
	n, reason = sampleUntil(0, time.Second, time.Now, nil, func() string { return "no" })
	if n != 0 || reason != "" {
		t.Fatalf("a zero window must not check: %d %q", n, reason)
	}
}

func TestReportFromKeepsTheSkip(t *testing.T) {
	run := RunView{
		ID: 4, Status: "partial", Summary: "1 ok, 0 failed, 1 skipped",
		Options: RunOptions{CanaryServerIDs: []int64{7}, ObserveSeconds: 30},
		Servers: []RunServerView{
			{ServerID: 7, ServerName: "web", Status: "succeeded", Detail: store.MustJSON(map[string]any{
				"upgrade": map[string]any{"packages": []string{"openssl"}},
				"checks":  []CheckResult{{Name: "ssh", Phase: "after", State: "pass"}},
			})},
			{ServerID: 8, ServerName: "db", Status: "skipped", Error: "stopped after the watch: web: connection refused"},
		},
	}
	got := reportFrom(run)
	if len(got.Servers) != 2 || !got.Servers[0].Canary || got.Servers[0].Packages != 1 || got.Servers[1].Status != "skipped" {
		t.Fatalf("%+v", got)
	}
	if got.ObserveSeconds != 30 || len(got.Servers[0].Checks) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestPlaygroundDial(t *testing.T) {
	host := os.Getenv("ESA_PLAYGROUND_HOST")
	if host == "" {
		t.Skip("set ESA_PLAYGROUND_HOST to dial a real server")
	}
	s := &Service{allowPrivate: os.Getenv("ESA_PLAYGROUND_PRIVATE") == "1"}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, "22"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 24)
	n, err := conn.Read(buf)
	_ = conn.Close()
	if err != nil || !strings.HasPrefix(string(buf[:n]), "SSH-") {
		t.Fatalf("port 22 did not send an SSH banner (%q, %v). A path that accepts every port is not this test.", buf[:n], err)
	}
	if got := s.checkTCP(ctx, host, 22); got != "" {
		t.Fatalf("tcp 22: %s", got)
	}
	if got := s.checkTCP(ctx, host, 443); got != "" {
		t.Fatalf("tcp 443: %s", got)
	}
	if got := s.checkTCP(ctx, host, 1); got == "" {
		t.Fatal("tcp 1 should fail")
	} else {
		t.Logf("tcp 1: %s", got)
	}
	http80 := s.checkHTTP(ctx, host, HealthCheck{Method: http.MethodGet, Path: "/", Port: 80})
	t.Logf("http 80: %q", http80)
	https := s.checkHTTP(ctx, host, HealthCheck{Method: http.MethodGet, Path: "/", Port: 443, TLS: true})
	t.Logf("https 443: %q", https)
	if strings.Contains(https, "169.254.169.254") || strings.Contains(http80, "169.254.169.254") {
		t.Fatal("a check followed a redirect")
	}
	if got := unitState([]proto.Service{{Unit: "ssh.service", Active: "active", Sub: "running"}}, "ssh.service"); got != "" {
		t.Fatal(got)
	}
	if got := containerState([]proto.Container{{Name: "traefik", State: "running"}}, "traefik"); got != "" {
		t.Fatal(got)
	}
	if got := containerState([]proto.Container{{Name: "traefik", State: "running"}}, "missing"); got != "not found" {
		t.Fatalf("missing container: %q", got)
	}
}

func TestCheckStateSeparatesFailureFromUnknown(t *testing.T) {
	if checkState("") != "pass" || checkState("HTTP 500") != "fail" {
		t.Fatal("pass and fail")
	}
	if checkState("address blocked") != "unknown" {
		t.Fatal("a blocked address was recorded as the application failing")
	}
	if checkState("not loaded") != "unconfigured" || checkState("not found") != "unconfigured" {
		t.Fatal("a missing unit or container was recorded as a crash")
	}
}

func splitTestHost(t *testing.T, raw string) (string, int) {
	t.Helper()
	u := strings.TrimPrefix(raw, "http://")
	host, portText, err := net.SplitHostPort(u)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}
