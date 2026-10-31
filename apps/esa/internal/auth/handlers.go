package auth

import (
	"log/slog"
	"net/http"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/config"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

// devIdentity is the single user of ESA_AUTH_MODE=dev.
var devIdentity = fleet.Identity{
	Issuer:  "dev",
	Subject: "dev",
	Email:   "dev@localhost",
	Name:    "Dev Admin",
}

// Routes registers the browser-facing sign-in endpoints under /auth/.
//
//	GET  /auth/login      start sign-in (OIDC mode)
//	GET  /auth/callback   provider redirect target (OIDC mode)
//	POST /auth/dev-login  sign in as the dev user (dev mode)
//	POST /auth/logout     end the session
func Routes(mux *http.ServeMux, mode config.AuthMode, oidc *OIDC, sessions *Sessions, fl *fleet.Service, log *slog.Logger) {
	switch mode {
	case config.AuthOIDC:
		mux.HandleFunc("GET /auth/login", oidc.Login)
		mux.HandleFunc("GET /auth/callback", oidc.Callback)
	case config.AuthDev:
		mux.HandleFunc("GET /auth/login", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/login", http.StatusFound)
		})
		mux.HandleFunc("POST /auth/dev-login", func(w http.ResponseWriter, r *http.Request) {
			res, err := fl.SignIn(r.Context(), devIdentity, fleet.SourceWeb)
			if err != nil {
				log.Error("dev sign in", "err", err)
				loginError(w, r, "failed")
				return
			}
			if err := sessions.Start(r.Context(), w, res.User.ID, res.AccountID); err != nil {
				log.Error("start session", "err", err)
				loginError(w, r, "failed")
				return
			}
			http.Redirect(w, r, safeReturnTo(r.FormValue("return_to")), http.StatusSeeOther)
		})
	}

	mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if err := sessions.End(r.Context(), w, r); err != nil {
			log.Error("end session", "err", err)
		}
		http.Redirect(w, r, "/login?signed_out=1", http.StatusSeeOther)
	})
}
