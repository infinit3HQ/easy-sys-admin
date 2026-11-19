package auth

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/config"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/password"
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
//	POST /auth/logout-all           end every session of the signed-in user
func Routes(mux *http.ServeMux, cfg config.Config, oidc map[string]*OIDC, sessions *Sessions, fl *fleet.Service, log *slog.Logger) {
	a := cfg.Auth
	start := func(w http.ResponseWriter, r *http.Request, res fleet.SignInResult) {
		if err := sessions.Start(r.Context(), w, r, res.User.ID, res.AccountID); err != nil {
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
		mux.HandleFunc("POST /auth/password/login", func(w http.ResponseWriter, r *http.Request) {
			email, err := fleet.NormalizeEmail(r.FormValue("email"))
			if err != nil {
				email = strings.ToLower(strings.TrimSpace(r.FormValue("email")))
			}
			ip := clientIP(r)
			if code := fl.SignInBlocked(email, ip); code != "" {
				loginError(w, r, code)
				return
			}
			res, err := fl.SignInPassword(r.Context(), email, r.FormValue("password"))
			switch {
			case errors.Is(err, fleet.ErrSignInNotAllowed):
				loginError(w, r, "not_allowed")
				return
			case errors.Is(err, fleet.ErrEmailUnconfirmed):
				http.Redirect(w, r, "/login?mode=verify&email="+url.QueryEscape(email), http.StatusSeeOther)
				return
			case busy(err):
				log.Warn("password sign in", "err", err)
				loginError(w, r, "busy")
				return
			case err != nil:
				if !errors.Is(err, fleet.ErrBadCredentials) {
					log.Error("password sign in", "err", err)
				}
				fl.SignInFail(email, ip)
				loginError(w, r, "bad_password")
				return
			}
			fl.SignInSucceed(email, ip)
			start(w, r, res)
		})
		mux.HandleFunc("POST /auth/password/signup", func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r)
			if fl.AuthLimited("signup\n"+ip, 10, time.Hour) {
				signupError(w, r, "rate_limited")
				return
			}
			fl.AuthHit("signup\n" + ip)
			// Without ESA_SIGNUP the fleet service still lets the first
			// user of an empty database sign up.
			res, err := fl.SignUpPassword(r.Context(), r.FormValue("name"), r.FormValue("email"), r.FormValue("password"))
			var verr *fleet.ValidationError
			switch {
			case errors.Is(err, fleet.ErrSignupClosed):
				signupError(w, r, "signup_closed")
				return
			case busy(err):
				log.Warn("password sign up", "err", err)
				signupError(w, r, "busy")
				return
			case errors.Is(err, fleet.ErrEmailTaken):
				signupError(w, r, "email_taken")
				return
			case errors.Is(err, fleet.ErrProviderAccount):
				signupError(w, r, "use_provider")
				return
			case errors.Is(err, fleet.ErrSignInNotAllowed):
				signupError(w, r, "not_allowed")
				return
			case errors.As(err, &verr):
				signupError(w, r, "invalid_"+verr.Field)
				return
			case errors.Is(err, fleet.ErrMailFailed):
				log.Error("confirmation email", "err", err)
				http.Redirect(w, r, "/login?mode=verify&error=mail_failed&email="+url.QueryEscape(formEmail(r)), http.StatusSeeOther)
				return
			case err != nil:
				log.Error("password sign up", "err", err)
				signupError(w, r, "failed")
				return
			}
			if res.NeedsConfirm {
				http.Redirect(w, r, "/login?mode=verify&sent=confirm&email="+url.QueryEscape(res.User.Email), http.StatusSeeOther)
				return
			}
			start(w, r, res)
		})
		if a.Mail {
			mux.HandleFunc("POST /auth/password/forgot", func(w http.ResponseWriter, r *http.Request) {
				email := formEmail(r)
				ip := clientIP(r)
				if fl.AuthLimited("mail\n"+email, 3, time.Hour) || fl.AuthLimited("mail-ip\n"+ip, 10, time.Hour) {
					http.Redirect(w, r, "/login?mode=forgot&error=rate_limited", http.StatusSeeOther)
					return
				}
				fl.AuthHit("mail\n" + email)
				fl.AuthHit("mail-ip\n" + ip)
				if err := fl.RequestPasswordReset(r.Context(), r.FormValue("email")); err != nil {
					var verr *fleet.ValidationError
					if errors.As(err, &verr) {
						http.Redirect(w, r, "/login?mode=forgot&error=invalid_email", http.StatusSeeOther)
						return
					}
					log.Error("password reset email", "err", err)
					http.Redirect(w, r, "/login?mode=forgot&error=mail_failed", http.StatusSeeOther)
					return
				}
				http.Redirect(w, r, "/login?sent=reset", http.StatusSeeOther)
			})
			mux.HandleFunc("POST /auth/email/resend", func(w http.ResponseWriter, r *http.Request) {
				email := formEmail(r)
				ip := clientIP(r)
				if fl.AuthLimited("mail\nconfirm:"+email, 3, time.Hour) || fl.AuthLimited("mail-ip\nconfirm:"+ip, 10, time.Hour) {
					http.Redirect(w, r, verifyRedirect(email, "rate_limited", ""), http.StatusSeeOther)
					return
				}
				fl.AuthHit("mail\nconfirm:" + email)
				fl.AuthHit("mail-ip\nconfirm:" + ip)
				if err := fl.ResendConfirmation(r.Context(), r.FormValue("email")); err != nil {
					var verr *fleet.ValidationError
					if errors.As(err, &verr) {
						http.Redirect(w, r, verifyRedirect(email, "invalid_email", ""), http.StatusSeeOther)
						return
					}
					log.Error("confirmation email", "err", err)
					http.Redirect(w, r, verifyRedirect(email, "mail_failed", ""), http.StatusSeeOther)
					return
				}
				http.Redirect(w, r, verifyRedirect(email, "", "confirm"), http.StatusSeeOther)
			})
			mux.HandleFunc("POST /auth/email/confirm", func(w http.ResponseWriter, r *http.Request) {
				ip := clientIP(r)
				if fl.AuthLimited("submit\nconfirm:"+ip, 20, time.Hour) {
					http.Redirect(w, r, "/login?mode=confirm&error=rate_limited", http.StatusSeeOther)
					return
				}
				fl.AuthHit("submit\nconfirm:" + ip)
				if err := fl.ConfirmEmail(r.Context(), r.FormValue("token")); err != nil {
					http.Redirect(w, r, "/login?mode=confirm&error=confirm_invalid", http.StatusSeeOther)
					return
				}
				http.Redirect(w, r, "/login?confirmed=1", http.StatusSeeOther)
			})
			mux.HandleFunc("POST /auth/password/reset", func(w http.ResponseWriter, r *http.Request) {
				ip := clientIP(r)
				if fl.AuthLimited("submit\nreset:"+ip, 20, time.Hour) {
					http.Redirect(w, r, "/login?mode=reset&error=rate_limited", http.StatusSeeOther)
					return
				}
				fl.AuthHit("submit\nreset:" + ip)
				err := fl.ResetPassword(r.Context(), r.FormValue("token"), r.FormValue("password"))
				var verr *fleet.ValidationError
				switch {
				case errors.As(err, &verr):
					http.Redirect(w, r, "/login?mode=reset&error=invalid_"+verr.Field, http.StatusSeeOther)
				case err != nil:
					http.Redirect(w, r, "/login?mode=reset&error=reset_invalid", http.StatusSeeOther)
				default:
					http.Redirect(w, r, "/login?reset=1", http.StatusSeeOther)
				}
			})
		}
	}

	if a.Dev {
		mux.HandleFunc("POST /auth/dev-login", func(w http.ResponseWriter, r *http.Request) {
			if !loopbackRequest(r) {
				http.Error(w, "dev login only works from localhost", http.StatusForbidden)
				return
			}
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
	mux.HandleFunc("POST /auth/logout-all", func(w http.ResponseWriter, r *http.Request) {
		if err := sessions.EndAll(r.Context(), w); err != nil {
			log.Error("end all sessions", "err", err)
		}
		http.Redirect(w, r, "/login?signed_out=1", http.StatusSeeOther)
	})
}

// busy is true when the server had no capacity to check a password.
func busy(err error) bool {
	return errors.Is(err, password.ErrBusy) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// loopbackRequest reports whether the browser addressed the server by a
// loopback name, directly (Host) and through the web app's proxy
// (X-Forwarded-Host). Dev login checks it so a page on another site (DNS
// rebinding) or another machine on the LAN cannot sign in.
func loopbackRequest(r *http.Request) bool {
	hosts := []string{r.Host}
	for _, v := range r.Header.Values("X-Forwarded-Host") {
		hosts = append(hosts, strings.Split(v, ",")...)
	}
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if host, _, err := net.SplitHostPort(h); err == nil {
			h = host
		} else {
			h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
		}
		switch strings.ToLower(h) {
		case "localhost", "127.0.0.1", "::1":
		default:
			return false
		}
	}
	return true
}

func signupError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/login?mode=signup&error="+code, http.StatusSeeOther)
}

// verifyRedirect is the "check your email" page. sent is "confirm" after a
// message goes out. code is an error when it does not.
func verifyRedirect(email, code, sent string) string {
	q := url.Values{"mode": {"verify"}}
	if email != "" {
		q.Set("email", email)
	}
	if code != "" {
		q.Set("error", code)
	}
	if sent != "" {
		q.Set("sent", sent)
	}
	return "/login?" + q.Encode()
}

func formEmail(r *http.Request) string {
	email, err := fleet.NormalizeEmail(r.FormValue("email"))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	}
	return email
}
