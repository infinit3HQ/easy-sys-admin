package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRedactPath(t *testing.T) {
	for in, want := range map[string]string{
		"/enroll/enr_secret":                    "/enroll/[redacted]",
		"/enroll/enr_secret/helper":             "/enroll/[redacted]/helper",
		"/enroll/enr_secret/complete":           "/enroll/[redacted]/complete",
		"/enroll/":                              "/enroll/",
		"/api/v1/servers":                       "/api/v1/servers",
		"/api/v1/invites/inv_secret":            "/api/v1/invites/[redacted]",
		"/api/v1/invites/inv_secret/accept":     "/api/v1/invites/[redacted]/accept",
		"/api/v1/invites/":                      "/api/v1/invites/",
		"/api/v1/shares/shr_secret":             "/api/v1/shares/[redacted]",
		"/api/v1/shares/shr_secret/accept":      "/api/v1/shares/[redacted]/accept",
		"/invite/inv_secret":                    "/invite/[redacted]",
		"/share/shr_secret":                     "/share/[redacted]",
		"/api/v1/team/invites/12":               "/api/v1/team/invites/12",
		"/api/v1/group-shares/3":                "/api/v1/group-shares/3",
		"/api/v1/share-links/4":                 "/api/v1/share-links/4",
		"/api/v1/groups/2/shares":               "/api/v1/groups/2/shares",
		"/api/v1/invites/inv_secret/accept/x/y": "/api/v1/invites/[redacted]/accept/x/y",
	} {
		if got := redactPath(in); got != want {
			t.Errorf("redactPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLimitBodies(t *testing.T) {
	var got int
	h := limitBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = len(b)
	}))
	send := func(path string, n int, chunked bool) *httptest.ResponseRecorder {
		var body io.Reader = strings.NewReader(strings.Repeat("x", n))
		if chunked {
			body = io.MultiReader(body) // hides the length, like the web proxy
		}
		r := httptest.NewRequest("POST", path, body)
		if chunked {
			r.ContentLength = -1
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if w := send("/api/v1/tokens", 100, true); w.Code != http.StatusOK || got != 100 {
		t.Fatalf("small body: %d, handler read %d bytes", w.Code, got)
	}
	for _, chunked := range []bool{false, true} {
		got = -1
		w := send("/auth/password/login", maxBodyBytes+1, chunked)
		var e struct {
			Error struct{ Code string } `json:"error"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &e)
		if w.Code != http.StatusRequestEntityTooLarge || e.Error.Code != "too_large" || got != -1 {
			t.Errorf("chunked %v: %d %s, handler ran %v", chunked, w.Code, w.Body, got != -1)
		}
	}
	if w := send("/other", maxBodyBytes+1, false); w.Code != http.StatusOK {
		t.Errorf("paths outside /api, /auth, /enroll are not limited here: %d", w.Code)
	}
}
