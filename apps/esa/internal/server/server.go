// Package server wires the control plane together: store, fleet service,
// auth, API, and background jobs, behind one HTTP server.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/api"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/auth"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/config"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/version"
)

// Run starts the control plane and blocks until ctx is cancelled, then shuts
// down gracefully.
func Run(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	st, err := store.Open(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Migrate(ctx, log); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	fl := fleet.New(st, fleet.Options{AllowedEmails: cfg.Auth.AllowedEmails, Logger: log})
	sessions := auth.NewSessions(st, fl, cfg.SecureCookies(), log)
	oidc := auth.NewOIDC(ctx, cfg, sessions, fl, log)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health(st))
	auth.Routes(mux, cfg.Auth.Mode, oidc, sessions, fl, log)
	api.Mount(mux, fl, sessions, cfg.Auth, log)

	handler := chain(mux,
		recoverPanics(log),
		logRequests(log),
		securityHeaders,
		crossOriginProtection(cfg),
		sessions.Middleware,
	)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	go runEvery(ctx, time.Hour, sessions.CleanupExpired)

	errc := make(chan error, 1)
	go func() {
		log.Info("control plane listening",
			"addr", cfg.Listen, "public_url", cfg.PublicURL.String(),
			"auth", cfg.Auth.Mode, "version", version.String())
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func health(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		status, code := "ok", http.StatusOK
		if err := st.Ping(ctx); err != nil {
			status, code = "database unavailable", http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": status, "version": version.Version})
	}
}

func runEvery(ctx context.Context, every time.Duration, fn func(context.Context)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		fn(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
