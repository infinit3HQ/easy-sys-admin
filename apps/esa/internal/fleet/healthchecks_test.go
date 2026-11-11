package fleet

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

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
