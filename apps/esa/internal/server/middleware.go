package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/api"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/config"
)

type middleware func(http.Handler) http.Handler

// chain applies middlewares so the first one listed is the outermost.
func chain(h http.Handler, mws ...middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func recoverPanics(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					log.Error("panic in handler", "path", redactPath(r.URL.Path), "panic", v, "stack", string(debug.Stack()))
					http.Error(w, "internal error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// logRequests logs method, path, status, and duration. It never logs query
// strings, because OIDC callbacks carry the authorization code there, and
// it hides enroll, invite, and share tokens in paths.
func logRequests(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			// Health checks and metric scrapes would flood the log; only failures are kept.
			if (r.URL.Path == "/healthz" || r.URL.Path == "/metrics") && rec.status == http.StatusOK {
				return
			}
			log.Info("http request",
				"method", r.Method,
				"path", redactPath(r.URL.Path),
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds())
		})
	}
}

// redactPath hides secret tokens in request paths (see api.RedactPath).
func redactPath(p string) string { return api.RedactPath(p) }

// maxBodyBytes caps request bodies for the API, sign-in forms, and enroll.
const maxBodyBytes = 1 << 20

// maxScanBytes is the cap for a scan report imported for one server.
const maxScanBytes = 10 << 20

var scanImportPath = regexp.MustCompile(`^/api/v1/servers/[0-9]+/scan$`)

// bodyLimit is the cap for one request path.
func bodyLimit(p string) int64 {
	if scanImportPath.MatchString(p) {
		return maxScanBytes
	}
	return maxBodyBytes
}

// limitBodies answers 413 in the API's error format when a body is over
// maxBodyBytes. The body is read up front because the web app's proxy
// forwards it chunked, without a Content-Length to check.
func limitBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if r.ContentLength == 0 || !(strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/auth/") || strings.HasPrefix(p, "/enroll/")) {
			next.ServeHTTP(w, r)
			return
		}
		limit := bodyLimit(p)
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			writeJSONError(w, http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("The request body is larger than %d MiB.", limit>>20))
			return
		case err != nil:
			writeJSONError(w, http.StatusBadRequest, "bad_request", "The request body could not be read.")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

// writeJSONError writes the API's error shape: {"error":{"code","message"}}.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// securityHeaders sets headers for the JSON API and auth redirects. The web
// app sets its own Content-Security-Policy for HTML pages.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// crossOriginProtection rejects cross-site state-changing requests (CSRF)
// using the browser's Sec-Fetch-Site and Origin headers. Requests without
// those headers, such as the CLI, are allowed and authenticate with tokens.
func crossOriginProtection(cfg config.Config) middleware {
	cop := http.NewCrossOriginProtection()
	// The web app's own origin is trusted even when the browser reaches the
	// API through the web server's proxy on another port during development.
	_ = cop.AddTrustedOrigin(cfg.PublicURL.String())
	return cop.Handler
}
