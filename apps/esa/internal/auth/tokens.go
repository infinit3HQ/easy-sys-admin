package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

// BearerMiddleware authenticates `Authorization: Bearer esa_...` requests
// (the CLI and MCP clients). It runs before the session middleware; a
// request with a bearer header never falls back to cookies.
//
// Clients set `X-Esa-Client: mcp` to have their calls recorded as MCP in
// the audit log. The header only labels the source; it grants nothing.
func BearerMiddleware(fl *fleet.Service, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if h == "" {
				next.ServeHTTP(w, r)
				return
			}
			secret, ok := strings.CutPrefix(h, "Bearer ")
			if !ok {
				unauthorized(w, "Use an Authorization: Bearer token.")
				return
			}
			source := fleet.SourceAPI
			if strings.EqualFold(r.Header.Get("X-Esa-Client"), "mcp") {
				source = fleet.SourceMCP
			}
			actor, ok, err := fl.AuthenticateToken(r.Context(), strings.TrimSpace(secret), source)
			if err != nil {
				log.Error("authenticate token", "err", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if !ok {
				unauthorized(w, "The token is invalid, expired, or revoked.")
				return
			}
			// Drop cookies so the session middleware cannot override the token.
			r.Header.Del("Cookie")
			next.ServeHTTP(w, r.WithContext(fleet.WithActor(r.Context(), actor)))
		})
	}
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="esa"`)
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "unauthorized", "message": msg}})
}
