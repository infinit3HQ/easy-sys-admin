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
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/crypto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/notify"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/version"
)

// Run starts the control plane and blocks until ctx is cancelled, then shuts
// down gracefully.
func Run(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	box, err := crypto.NewBox(cfg.MasterKeys...)
	if err != nil {
		return fmt.Errorf("ESA_MASTER_KEY: %w", err)
	}
	st, err := store.Open(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx, log); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	pool := remote.NewPool()
	defer pool.Close()
	helpers := newHelperFiles(cfg.HelperDir)
	for _, arch := range []string{"amd64", "arm64"} {
		if _, err := helpers.Get(arch); err != nil {
			log.Warn("helper binary missing; adding servers of this architecture will fail", "arch", arch, "err", err)
		}
	}

	// Background work gets its own context, cancelled after the HTTP server
	// has stopped, so in-flight requests can still start runs during shutdown.
	bgCtx, stopBackground := context.WithCancel(context.WithoutCancel(ctx))
	defer stopBackground()

	fl := fleet.New(bgCtx, fleet.Deps{
		Store: st, Box: box, Pool: pool, Helpers: helpers.Get, Notifier: notify.New(cfg.AllowPrivateNetworks),
		PublicURL: cfg.PublicURL, AllowedEmails: cfg.Auth.AllowedEmails, Signup: cfg.Auth.Signup,
		AllowPrivateNetworks: cfg.AllowPrivateNetworks, Logger: log,
	})
	sessions := auth.NewSessions(st, fl, cfg.SecureCookies(), log)
	oidc := map[string]*auth.OIDC{}
	for _, p := range cfg.Auth.Providers {
		oidc[p.ID] = auth.NewOIDC(ctx, cfg, p, sessions, fl, log)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health(st))
	auth.Routes(mux, cfg, oidc, sessions, fl, log)
	api.Mount(mux, fl, sessions, cfg.Auth, log)
	api.MountEnroll(mux, fl, log)

	handler := chain(mux,
		recoverPanics(log),
		logRequests(log),
		securityHeaders,
		limitBodies,
		crossOriginProtection(cfg),
		auth.BearerMiddleware(fl, log),
		sessions.Middleware,
	)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		// Some calls wait on servers (apt-get update, image checks, helper installs).
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  120 * time.Second,
		BaseContext:  func(net.Listener) context.Context { return ctx },
		ErrorLog:     slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	startJobs(bgCtx, fl, sessions, log)

	if cfg.Auth.SignupIgnored {
		log.Warn("ESA_SIGNUP=true is ignored because ESA_ALLOWED_EMAILS is set: a password sign-up cannot prove the person owns the address. " +
			"Allowed people should sign in with OIDC or Google; only the first user of an empty database can create a password account.")
	}

	errc := make(chan error, 1)
	go func() {
		log.Info("control plane listening",
			"addr", cfg.Listen, "public_url", cfg.PublicURL.String(),
			"dev_login", cfg.Auth.Dev, "password", cfg.Auth.Password, "signup", cfg.Auth.Signup,
			"providers", len(cfg.Auth.Providers), "version", version.String())
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	stopBackground()
	waitOrTimeout(fl.Wait, 20*time.Second, log)
	return err
}

func waitOrTimeout(wait func(), d time.Duration, log *slog.Logger) {
	done := make(chan struct{})
	go func() { wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		log.Warn("background work still running at shutdown; upgrades continue on the servers and resume on next start")
	}
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
