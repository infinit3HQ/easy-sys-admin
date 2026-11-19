package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestBodyLimit(t *testing.T) {
	if bodyLimit("/api/v1/servers/12/scan") != maxScanBytes {
		t.Fatal("the scan import needs the larger cap")
	}
	for _, p := range []string{"/api/v1/servers/12/scan/x", "/api/v1/servers/abc/scan", "/api/v1/plans", "/auth/login"} {
		if bodyLimit(p) != maxBodyBytes {
			t.Fatalf("%s must keep 1 MiB", p)
		}
	}
}

// A large body is read before the request is authenticated, so a request
// that names no caller is turned away before anything is buffered.
func TestLargeBodiesNeedACaller(t *testing.T) {
	reached := false
	h := limitBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	big := strings.Repeat("x", 4096)
	for _, c := range []struct {
		path   string
		header map[string]string
		want   int
	}{
		{"/api/v1/agent/result", nil, http.StatusUnauthorized},
		{"/api/v1/servers/3/scan", nil, http.StatusUnauthorized},
		{"/api/v1/agent/result", map[string]string{"X-Esa-Agent-Token": "esa_agent_x"}, http.StatusOK},
		{"/api/v1/servers/3/scan", map[string]string{"Authorization": "Bearer esa_x"}, http.StatusOK},
	} {
		reached = false
		req := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(big))
		for k, v := range c.header {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want && !(c.want == http.StatusOK && reached) {
			t.Errorf("%s with %v: status %d reached=%v, want %d", c.path, c.header, rec.Code, reached, c.want)
		}
		if c.want == http.StatusUnauthorized && reached {
			t.Errorf("%s reached the handler without credentials", c.path)
		}
	}
}

// Only a few large bodies are held in memory at once, and one client cannot
// take them all.
func TestLargeBodiesAreBounded(t *testing.T) {
	var releases []func()
	t.Cleanup(func() {
		for _, r := range releases {
			r()
		}
	})
	h := limitBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	post := func(client string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/result", strings.NewReader("{}"))
		req.Header.Set("X-Esa-Agent-Token", "esa_agent_x")
		req.Header.Set("X-Forwarded-For", client)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	// One client holds its share, then is told to wait.
	for range maxLargeBodiesPerClient {
		r := acquireLargeBody("10.0.0.1")
		if r == nil {
			t.Fatal("a client could not take its own share")
		}
		releases = append(releases, r)
	}
	if code := post("10.0.0.1"); code != http.StatusServiceUnavailable {
		t.Fatalf("a client over its share got %d, want 503", code)
	}
	// Others are not affected by that client.
	if code := post("10.0.0.2"); code != http.StatusOK {
		t.Fatalf("another client got %d while one was over its share", code)
	}
	// With every slot taken by different clients, a new one waits.
	for i := len(releases); i < maxLargeBodies; i++ {
		r := acquireLargeBody("10.1.0." + strconv.Itoa(i))
		if r == nil {
			t.Fatal("a slot was not free")
		}
		releases = append(releases, r)
	}
	if code := post("10.0.0.3"); code != http.StatusServiceUnavailable {
		t.Fatalf("status %d with every slot taken, want 503", code)
	}
}
