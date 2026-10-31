package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/auth"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/config"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

// Handlers implements StrictServerInterface on top of the fleet service.
// Handlers return fleet errors as-is; errorStatus maps them to HTTP.
type Handlers struct {
	fleet    *fleet.Service
	sessions *auth.Sessions
	auth     config.AuthConfig
	log      *slog.Logger
}

var _ StrictServerInterface = (*Handlers)(nil)

var errUnauthenticated = errors.New("unauthenticated")

// Mount registers /api/v1/* on mux. Requests must already have passed
// through the bearer and session middlewares.
func Mount(mux *http.ServeMux, fl *fleet.Service, sessions *auth.Sessions, authCfg config.AuthConfig, log *slog.Logger) {
	h := &Handlers{fleet: fl, sessions: sessions, auth: authCfg, log: log}
	strict := NewStrictHandlerWithOptions(h, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, http.StatusBadRequest, "bad_request", "The request body is not valid JSON for this endpoint.", "")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			status, code, msg, field := errorStatus(err)
			if status == http.StatusInternalServerError {
				log.Error("api handler", "path", RedactPath(r.URL.Path), "err", err)
			}
			writeError(w, status, code, msg, field)
		},
	})
	HandlerWithOptions(strict, StdHTTPServerOptions{
		BaseURL:    "/api/v1",
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error(), "")
		},
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "No such API endpoint.", "")
	})
}

// tokenPathPrefixes are paths whose next segment is a secret token. Enroll
// tokens let anyone enroll a server, invite and share tokens let anyone join
// a team or receive a group, until they are used. /invite/ and /share/ are
// the web app's pages; they are listed in case they ever reach Go.
var tokenPathPrefixes = []string{"/enroll/", "/api/v1/invites/", "/api/v1/shares/", "/invite/", "/share/"}

// RedactPath replaces the token segment of the paths in tokenPathPrefixes
// with [redacted], so request and error logs never carry those tokens.
func RedactPath(p string) string {
	for _, prefix := range tokenPathPrefixes {
		rest, ok := strings.CutPrefix(p, prefix)
		if !ok || rest == "" {
			continue
		}
		if _, tail, found := strings.Cut(rest, "/"); found {
			return prefix + "[redacted]/" + tail
		}
		return prefix + "[redacted]"
	}
	return p
}

func errorStatus(err error) (status int, code, msg, field string) {
	var verr *fleet.ValidationError
	var conflict *fleet.ConflictError
	var unavail *fleet.UnavailableError
	switch {
	case errors.Is(err, errUnauthenticated):
		return http.StatusUnauthorized, "unauthorized", "Sign in, or send an API token.", ""
	case errors.As(err, &verr):
		return http.StatusBadRequest, "bad_request", verr.Message, verr.Field
	case errors.Is(err, fleet.ErrForbidden):
		return http.StatusForbidden, "forbidden", "Your role or token does not allow this.", ""
	case errors.Is(err, fleet.ErrNotFound):
		return http.StatusNotFound, "not_found", "Not found.", ""
	case errors.As(err, &conflict):
		return http.StatusConflict, "conflict", conflict.Message, ""
	case errors.As(err, &unavail):
		return http.StatusBadGateway, "unavailable", unavail.Message, ""
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "unavailable", "The server took too long to answer.", ""
	}
	return http.StatusInternalServerError, "internal", "Something went wrong on the control plane.", ""
}

func writeError(w http.ResponseWriter, status int, code, message, field string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Error{Error: ErrorDetail{Code: code, Message: message, Field: optional(field)}})
}

func actor(ctx context.Context) (fleet.Actor, error) {
	a, ok := fleet.ActorFrom(ctx)
	if !ok {
		return fleet.Actor{}, errUnauthenticated
	}
	return a, nil
}

// ---- Session ---------------------------------------------------------------------

func (h *Handlers) GetAuthConfig(ctx context.Context, _ GetAuthConfigRequestObject) (GetAuthConfigResponseObject, error) {
	// Sign-up is open when ESA_SIGNUP is on, or for the first user of an empty install.
	open, err := h.fleet.SignupOpen(ctx)
	if err != nil {
		return nil, err
	}
	providers := make([]AuthProvider, 0, len(h.auth.Providers))
	for _, p := range h.auth.Providers {
		providers = append(providers, AuthProvider{Id: p.ID, Name: p.Name})
	}
	return GetAuthConfig200JSONResponse{Dev: h.auth.Dev, Password: h.auth.Password, Signup: h.auth.Password && open, Providers: providers}, nil
}

func (h *Handlers) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	me, err := h.fleet.Me(ctx, a)
	if err != nil {
		if errors.Is(err, fleet.ErrForbidden) {
			return nil, errUnauthenticated
		}
		return nil, err
	}
	return GetMe200JSONResponse(toMe(me, a)), nil
}

func (h *Handlers) SwitchAccount(ctx context.Context, req SwitchAccountRequestObject) (SwitchAccountResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if a.TokenID != 0 {
		return nil, &fleet.ValidationError{Field: "accountId", Message: "API tokens belong to one account; create a token in the other account instead"}
	}
	if req.Body == nil || req.Body.AccountId <= 0 {
		return nil, &fleet.ValidationError{Field: "accountId", Message: "is required"}
	}
	to, err := h.sessions.SwitchAccount(ctx, req.Body.AccountId)
	if err != nil {
		return nil, err
	}
	a.AccountID, a.Role = to.Account.ID, fleet.Role(to.Role)
	me, err := h.fleet.Me(ctx, a)
	if err != nil {
		return nil, err
	}
	return SwitchAccount200JSONResponse(toMe(me, a)), nil
}

func (h *Handlers) GetFleetOverview(ctx context.Context, _ GetFleetOverviewRequestObject) (GetFleetOverviewResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ov, err := h.fleet.Overview(ctx, a)
	if err != nil {
		return nil, err
	}
	return GetFleetOverview200JSONResponse(toOverview(ov)), nil
}
