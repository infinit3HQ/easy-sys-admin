package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/auth"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/config"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// Handlers implements StrictServerInterface on top of the fleet service.
type Handlers struct {
	fleet    *fleet.Service
	sessions *auth.Sessions
	auth     config.AuthConfig
	log      *slog.Logger
}

var _ StrictServerInterface = (*Handlers)(nil)

// Mount registers /api/v1/* on mux. Requests must already have passed
// through the session middleware.
func Mount(mux *http.ServeMux, fl *fleet.Service, sessions *auth.Sessions, authCfg config.AuthConfig, log *slog.Logger) {
	h := &Handlers{fleet: fl, sessions: sessions, auth: authCfg, log: log}
	strict := NewStrictHandlerWithOptions(h, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, http.StatusBadRequest, "bad_request", "The request body is not valid JSON for this endpoint.")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Error("api handler", "path", r.URL.Path, "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "Something went wrong on the control plane.")
		},
	})
	HandlerWithOptions(strict, StdHTTPServerOptions{
		BaseURL:    "/api/v1",
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		},
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "No such API endpoint.")
	})
}

// GetAuthConfig tells the sign-in page which button to show.
func (h *Handlers) GetAuthConfig(context.Context, GetAuthConfigRequestObject) (GetAuthConfigResponseObject, error) {
	return GetAuthConfig200JSONResponse{
		Mode:         AuthConfigMode(h.auth.Mode),
		ProviderName: h.auth.ProviderName,
	}, nil
}

// GetMe returns the session's user and accounts.
func (h *Handlers) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	actor, ok := fleet.ActorFrom(ctx)
	if !ok {
		return GetMe401JSONResponse{unauthorized()}, nil
	}
	me, err := h.fleet.Me(ctx, actor)
	if errors.Is(err, fleet.ErrForbidden) || errors.Is(err, store.ErrNotFound) {
		return GetMe401JSONResponse{unauthorized()}, nil
	}
	if err != nil {
		return nil, err
	}
	return GetMe200JSONResponse(toMe(me)), nil
}

// SwitchAccount moves the session to another of the user's accounts.
func (h *Handlers) SwitchAccount(ctx context.Context, req SwitchAccountRequestObject) (SwitchAccountResponseObject, error) {
	actor, ok := fleet.ActorFrom(ctx)
	if !ok {
		return SwitchAccount401JSONResponse{unauthorized()}, nil
	}
	if req.Body == nil || req.Body.AccountId <= 0 {
		return SwitchAccount400JSONResponse{BadRequestJSONResponse(errorBody("bad_request", "accountId is required."))}, nil
	}
	to, err := h.sessions.SwitchAccount(ctx, req.Body.AccountId)
	if errors.Is(err, fleet.ErrForbidden) {
		return SwitchAccount403JSONResponse{ForbiddenJSONResponse(errorBody("forbidden", "You are not a member of that account."))}, nil
	}
	if err != nil {
		return nil, err
	}
	actor.AccountID, actor.Role = to.Account.ID, fleet.Role(to.Role)
	me, err := h.fleet.Me(ctx, actor)
	if err != nil {
		return nil, err
	}
	return SwitchAccount200JSONResponse(toMe(me)), nil
}

// GetFleetOverview returns the overview of the session's account.
func (h *Handlers) GetFleetOverview(ctx context.Context, _ GetFleetOverviewRequestObject) (GetFleetOverviewResponseObject, error) {
	actor, ok := fleet.ActorFrom(ctx)
	if !ok {
		return GetFleetOverview401JSONResponse{unauthorized()}, nil
	}
	ov, err := h.fleet.Overview(ctx, actor)
	if errors.Is(err, fleet.ErrForbidden) {
		return GetFleetOverview401JSONResponse{unauthorized()}, nil
	}
	if err != nil {
		return nil, err
	}
	return GetFleetOverview200JSONResponse(toOverview(ov)), nil
}

func unauthorized() UnauthorizedJSONResponse {
	return UnauthorizedJSONResponse(errorBody("unauthorized", "Sign in to continue."))
}

func errorBody(code, message string) Error {
	return Error{Error: ErrorDetail{Code: code, Message: message}}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody(code, message))
}
