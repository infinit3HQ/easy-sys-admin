package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

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
//	GET  /auth/login/{provider}     start sign-in with an OIDC provider (oidc, google)
//	GET  /auth/callback/{provider}  provider redirect target
//	POST /auth/password/login       email and password sign-in (form)
//	POST /auth/password/signup      create an email and password account (form)
//	POST /auth/dev-login            sign in as the dev user (dev mode)
//	POST /auth/logout               end the session
func Routes(mux *http.ServeMux, cfg config.Config, oidc map[string]*OIDC, sessions *Sessions, fl *fleet.Service, log *slog.Logger) {
	a := cfg.Auth
	start := func(w http.ResponseWriter, r *http.Request, res fleet.SignInResult) {
		if err := sessions.Start(r.Context(), w, res.User.ID, res.AccountID); err != nil {
			log.Error("start session", "err", err)
			loginError(w, r, "failed")
			return
		}
		http.Redirect(w, r, safeReturnTo(r.FormValue("return_to")), http.StatusSeeOther)
	}

	mux.HandleFunc("GET /auth/login/{provider}", func(w http.ResponseWriter, r *http.Request) {
		if p, ok := oidc[r.PathValue("provider")]; ok {
			p.Login(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /auth/callback/{provider}", func(w http.ResponseWriter, r *http.Request) {
		if p, ok := oidc[r.PathValue("provider")]; ok {
			p.Callback(w, r)
			return
		}
		http.NotFound(w, r)
	})

	if a.Password {
		// Five failures per address in 15 minutes, then a pause.
		limiter := newFailureLimiter(5, 15*time.Minute)
		mux.HandleFunc("POST /auth/password/login", func(w http.ResponseWriter, r *http.Request) {
			email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
			if limiter.Blocked(email) {
				loginError(w, r, "locked")
				return
			}
			res, err := fl.SignInPassword(r.Context(), email, r.FormValue("password"))
			switch {
			case errors.Is(err, fleet.ErrSignInNotAllowed):
				loginError(w, r, "not_allowed")
				return
			case err != nil:
				if !errors.Is(err, fleet.ErrBadCredentials) {
					log.Error("password sign in", "err", err)
				}
				limiter.Fail(email)
				loginError(w, r, "bad_password")
				return
			}
			limiter.Reset(email)
			start(w, r, res)
		})
		mux.HandleFunc("POST /auth/password/signup", func(w http.ResponseWriter, r *http.Request) {
			if !a.Signup {
				signupError(w, r, "signup_closed")
				return
			}
			res, err := fl.SignUpPassword(r.Context(), r.FormValue("name"), r.FormValue("email"), r.FormValue("password"))
			var verr *fleet.ValidationError
			switch {
			case errors.Is(err, fleet.ErrEmailTaken):
				signupError(w, r, "email_taken")
				return
			case errors.Is(err, fleet.ErrSignInNotAllowed):
				signupError(w, r, "not_allowed")
				return
			case errors.As(err, &verr):
				signupError(w, r, "invalid_"+verr.Field)
				return
			case err != nil:
				log.Error("password sign up", "err", err)
				signupError(w, r, "failed")
				return
			}
			start(w, r, res)
		})
	}

	if a.Dev {
		mux.HandleFunc("POST /auth/dev-login", func(w http.ResponseWriter, r *http.Request) {
			res, err := fl.SignIn(r.Context(), devIdentity, fleet.SourceWeb)
			if err != nil {
				log.Error("dev sign in", "err", err)
				loginError(w, r, "failed")
				return
			}
			start(w, r, res)
		})
	}

	mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if err := sessions.End(r.Context(), w, r); err != nil {
			log.Error("end session", "err", err)
		}
		http.Redirect(w, r, "/login?signed_out=1", http.StatusSeeOther)
	})
}

func signupError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/login?mode=signup&error="+code, http.StatusSeeOther)
}
