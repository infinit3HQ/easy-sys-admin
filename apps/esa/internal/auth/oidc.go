package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/config"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

const loginStateTTL = 10 * time.Minute

// OIDC runs the authorization code flow with PKCE against one provider.
type OIDC struct {
	cfg         config.OIDCProvider
	redirectURL string
	secure      bool
	sessions    *Sessions
	fleet       *fleet.Service
	log         *slog.Logger
	httpClient  *http.Client
	// baseCtx outlives requests. go-oidc keeps it for later signing-key fetches.
	baseCtx context.Context

	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// NewOIDC returns the OIDC sign-in handlers. Provider discovery happens on
// the first login, so the control plane starts even when the provider is down.
func NewOIDC(baseCtx context.Context, cfg config.Config, provider config.OIDCProvider, sessions *Sessions, fl *fleet.Service, log *slog.Logger) *OIDC {
	return &OIDC{
		cfg:         provider,
		redirectURL: cfg.PublicURL.JoinPath("/auth/callback", provider.ID).String(),
		secure:      cfg.SecureCookies(),
		sessions:    sessions,
		fleet:       fl,
		log:         log.With("provider", provider.ID),
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		baseCtx:     baseCtx,
	}
}

func (o *OIDC) client() (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.oauth != nil {
		return o.oauth, o.verifier, nil
	}
	ctx := oidc.ClientContext(o.baseCtx, o.httpClient)
	provider, err := oidc.NewProvider(ctx, o.cfg.Issuer)
	if err != nil {
		return nil, nil, err
	}
	o.oauth = &oauth2.Config{
		ClientID:     o.cfg.ClientID,
		ClientSecret: o.cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  o.redirectURL,
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	o.verifier = provider.Verifier(&oidc.Config{ClientID: o.cfg.ClientID})
	return o.oauth, o.verifier, nil
}

type loginState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	ReturnTo string `json:"r"`
}

func (o *OIDC) stateCookieName() string {
	if o.secure {
		return "__Host-esa_oidc_" + o.cfg.ID
	}
	return "esa_oidc_" + o.cfg.ID
}

// Login redirects the browser to the provider.
func (o *OIDC) Login(w http.ResponseWriter, r *http.Request) {
	oauth, _, err := o.client()
	if err != nil {
		o.log.Error("oidc discovery", "issuer", o.cfg.Issuer, "err", err)
		loginError(w, r, "provider_unavailable")
		return
	}

	st := loginState{
		State:    randomString(),
		Nonce:    randomString(),
		Verifier: oauth2.GenerateVerifier(),
		ReturnTo: safeReturnTo(r.URL.Query().Get("return_to")),
	}
	raw, _ := json.Marshal(st)
	http.SetCookie(w, &http.Cookie{
		Name:     o.stateCookieName(),
		Value:    base64.RawURLEncoding.EncodeToString(raw),
		Path:     "/",
		MaxAge:   int(loginStateTTL.Seconds()),
		HttpOnly: true,
		Secure:   o.secure,
		SameSite: http.SameSiteLaxMode,
	})

	url := oauth.AuthCodeURL(st.State, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.Verifier))
	http.Redirect(w, r, url, http.StatusFound)
}

// Callback finishes the flow: checks state and nonce, verifies the ID token,
// signs the user in, and starts a session.
func (o *OIDC) Callback(w http.ResponseWriter, r *http.Request) {
	st, ok := o.takeState(w, r)
	if !ok {
		loginError(w, r, "state_mismatch")
		return
	}
	q := r.URL.Query()
	if q.Get("error") != "" {
		o.log.Info("oidc provider returned an error", "error", q.Get("error"))
		loginError(w, r, "denied")
		return
	}
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(st.State)) != 1 {
		loginError(w, r, "state_mismatch")
		return
	}

	oauth, verifier, err := o.client()
	if err != nil {
		o.log.Error("oidc discovery", "issuer", o.cfg.Issuer, "err", err)
		loginError(w, r, "provider_unavailable")
		return
	}

	ctx := context.WithValue(r.Context(), oauth2.HTTPClient, o.httpClient)
	tok, err := oauth.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(st.Verifier))
	if err != nil {
		o.log.Warn("oidc code exchange", "err", err)
		loginError(w, r, "failed")
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		o.log.Warn("oidc token response has no id_token")
		loginError(w, r, "failed")
		return
	}
	idToken, err := verifier.Verify(ctx, rawID)
	if err != nil {
		o.log.Warn("oidc id token verification", "err", err)
		loginError(w, r, "failed")
		return
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(st.Nonce)) != 1 {
		loginError(w, r, "state_mismatch")
		return
	}

	var claims struct {
		Email             string `json:"email"`
		EmailVerified     *bool  `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		o.log.Warn("oidc claims", "err", err)
		loginError(w, r, "failed")
		return
	}
	// An unverified address must not pass the email allow list.
	if claims.EmailVerified != nil && !*claims.EmailVerified {
		claims.Email = ""
	}
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}

	res, err := o.fleet.SignIn(r.Context(), fleet.Identity{
		Issuer:  idToken.Issuer,
		Subject: idToken.Subject,
		Email:   claims.Email,
		Name:    name,
	}, fleet.SourceWeb)
	if errors.Is(err, fleet.ErrSignInNotAllowed) {
		o.log.Info("sign-in refused by allow list", "email", claims.Email)
		loginError(w, r, "not_allowed")
		return
	}
	if err != nil {
		o.log.Error("sign in", "err", err)
		loginError(w, r, "failed")
		return
	}
	if err := o.sessions.Start(r.Context(), w, res.User.ID, res.AccountID); err != nil {
		o.log.Error("start session", "err", err)
		loginError(w, r, "failed")
		return
	}
	http.Redirect(w, r, st.ReturnTo, http.StatusSeeOther)
}

// takeState reads and clears the login state cookie.
func (o *OIDC) takeState(w http.ResponseWriter, r *http.Request) (loginState, bool) {
	c, err := r.Cookie(o.stateCookieName())
	http.SetCookie(w, &http.Cookie{
		Name: o.stateCookieName(), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: o.secure, SameSite: http.SameSiteLaxMode,
	})
	if err != nil {
		return loginState{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return loginState{}, false
	}
	var st loginState
	if err := json.Unmarshal(raw, &st); err != nil || st.State == "" || st.Nonce == "" || st.Verifier == "" {
		return loginState{}, false
	}
	st.ReturnTo = safeReturnTo(st.ReturnTo)
	return st, true
}

func randomString() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
