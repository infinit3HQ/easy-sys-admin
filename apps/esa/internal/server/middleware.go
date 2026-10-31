package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

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
					log.Error("panic in handler", "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
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
// it hides enroll tokens in paths.
func logRequests(log *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			if r.URL.Path == "/healthz" && rec.status == http.StatusOK {
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

// redactPath replaces the enroll token in /enroll/{token}/... paths, because
// the token lets anyone enroll a server until it is used.
func redactPath(p string) string {
	rest, ok := strings.CutPrefix(p, "/enroll/")
	if !ok || rest == "" {
		return p
	}
	_, tail, found := strings.Cut(rest, "/")
	if found {
		return "/enroll/[redacted]/" + tail
	}
	return "/enroll/[redacted]"
}

// maxBodyBytes caps request bodies for the API, sign-in forms, and enroll.
const maxBodyBytes = 1 << 20

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
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			writeJSONError(w, http.StatusRequestEntityTooLarge, "too_large", "The request body is larger than 1 MiB.")
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
