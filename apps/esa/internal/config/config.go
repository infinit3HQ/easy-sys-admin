// Package config reads control plane settings from the environment.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// Config is the control plane configuration.
type Config struct {
	// Listen is the address the HTTP server binds to.
	Listen string
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// PublicURL is the origin users open in the browser, for example https://esa.example.com.
	// The web app and the API are served from this one origin.
	PublicURL *url.URL
	// LogFormat is "text" or "json".
	LogFormat string
	// MasterKeys encrypt secrets at rest. The first seals new secrets; all
	// of them open existing ones, so keys can be rotated.
	MasterKeys []string
	// HelperDir holds esa-helper-linux-amd64 and esa-helper-linux-arm64.
	HelperDir string
	// AllowPrivateNetworks lets servers, certificate checks, and notification
	// channels point at private addresses (10/8, 172.16/12, 192.168/16, ...).
	// Loopback, link-local, and cloud metadata addresses are always refused.
	// Defaults to true when signup is off, false when anyone can sign up.
	AllowPrivateNetworks bool

	Auth AuthConfig
}

// AuthConfig holds sign-in settings. Any combination of methods can be on.
type AuthConfig struct {
	// Dev signs everyone in as one local user. Only allowed on loopback.
	Dev bool
	// Password enables email and password sign-in.
	Password bool
	// Signup lets new people create an email and password account.
	Signup bool
	// Providers are OpenID Connect providers (Authentik, Google, ...).
	Providers []OIDCProvider
	// AllowedEmails limits who may sign in with any method. Empty allows everyone.
	AllowedEmails []string
}

// OIDCProvider is one OpenID Connect client.
type OIDCProvider struct {
	ID           string // in URLs: /auth/login/{id}
	Name         string // on the button
	Issuer       string
	ClientID     string
	ClientSecret string
}

// Provider returns the provider with the given id.
func (a AuthConfig) Provider(id string) (OIDCProvider, bool) {
	for _, p := range a.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return OIDCProvider{}, false
}

// SecureCookies is true when the public URL is HTTPS.
func (c Config) SecureCookies() bool { return c.PublicURL.Scheme == "https" }

// FromEnv reads the configuration from ESA_* environment variables and validates it.
func FromEnv() (Config, error) {
	return parse(os.Getenv)
}

func parse(getenv func(string) string) (Config, error) {
	get := func(key, fallback string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return fallback
	}

	cfg := Config{
		Listen:      get("ESA_LISTEN", "127.0.0.1:8080"),
		DatabaseURL: get("ESA_DATABASE_URL", ""),
		LogFormat:   get("ESA_LOG_FORMAT", "text"),
		Auth: AuthConfig{
			AllowedEmails: splitList(get("ESA_ALLOWED_EMAILS", "")),
		},
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("ESA_DATABASE_URL is required, for example postgres://esa:secret@localhost:5432/esa")
	}
	for _, k := range strings.Split(get("ESA_MASTER_KEY", ""), ",") {
		if k = strings.TrimSpace(k); k != "" {
			cfg.MasterKeys = append(cfg.MasterKeys, k)
		}
	}
	if len(cfg.MasterKeys) == 0 {
		return Config{}, errors.New("ESA_MASTER_KEY is required: 32 random bytes, base64-encoded (openssl rand -base64 32)")
	}
	cfg.HelperDir = get("ESA_HELPER_DIR", "./bin")

	publicURL, err := url.Parse(get("ESA_PUBLIC_URL", "http://localhost:3000"))
	if err != nil || (publicURL.Scheme != "http" && publicURL.Scheme != "https") || publicURL.Host == "" {
		return Config{}, errors.New("ESA_PUBLIC_URL must be an absolute http or https URL")
	}
	if publicURL.Path != "" && publicURL.Path != "/" {
		return Config{}, errors.New("ESA_PUBLIC_URL must be an origin without a path")
	}
	publicURL.Path = ""
	cfg.PublicURL = publicURL

	if cfg.LogFormat != "text" && cfg.LogFormat != "json" {
		return Config{}, fmt.Errorf("ESA_LOG_FORMAT must be text or json, got %q", cfg.LogFormat)
	}

	if err := parseAuth(&cfg, get, publicURL); err != nil {
		return Config{}, err
	}

	privateDefault := "true"
	if cfg.Auth.Signup {
		privateDefault = "false"
	}
	if cfg.AllowPrivateNetworks, err = parseBool(get("ESA_ALLOW_PRIVATE_NETWORKS", privateDefault)); err != nil {
		return Config{}, fmt.Errorf("ESA_ALLOW_PRIVATE_NETWORKS: %w", err)
	}

	return cfg, nil
}

func parseAuth(cfg *Config, get func(string, string) string, publicURL *url.URL) error {
	a := &cfg.Auth
	switch mode := get("ESA_AUTH_MODE", "standard"); mode {
	case "dev":
		a.Dev = true
		if !isLoopback(cfg.Listen) || !isLoopbackHost(publicURL.Hostname()) {
			return errors.New("ESA_AUTH_MODE=dev is only allowed when ESA_LISTEN and ESA_PUBLIC_URL are loopback addresses")
		}
	case "standard", "oidc":
	default:
		return fmt.Errorf("ESA_AUTH_MODE must be standard or dev, got %q", mode)
	}

	var err error
	if a.Password, err = parseBool(get("ESA_AUTH_PASSWORD", "true")); err != nil {
		return fmt.Errorf("ESA_AUTH_PASSWORD: %w", err)
	}
	if a.Signup, err = parseBool(get("ESA_SIGNUP", "true")); err != nil {
		return fmt.Errorf("ESA_SIGNUP: %w", err)
	}
	a.Signup = a.Signup && a.Password

	if issuer := get("ESA_OIDC_ISSUER", ""); issuer != "" {
		p := OIDCProvider{
			ID: "oidc", Name: get("ESA_OIDC_PROVIDER_NAME", "Authentik"), Issuer: issuer,
			ClientID: get("ESA_OIDC_CLIENT_ID", ""), ClientSecret: get("ESA_OIDC_CLIENT_SECRET", ""),
		}
		if p.ClientID == "" || p.ClientSecret == "" {
			return errors.New("ESA_OIDC_CLIENT_ID and ESA_OIDC_CLIENT_SECRET are required with ESA_OIDC_ISSUER")
		}
		a.Providers = append(a.Providers, p)
	}
	if id := get("ESA_GOOGLE_CLIENT_ID", ""); id != "" {
		secret := get("ESA_GOOGLE_CLIENT_SECRET", "")
		if secret == "" {
			return errors.New("ESA_GOOGLE_CLIENT_SECRET is required with ESA_GOOGLE_CLIENT_ID")
		}
		a.Providers = append(a.Providers, OIDCProvider{ID: "google", Name: "Google", Issuer: "https://accounts.google.com", ClientID: id, ClientSecret: secret})
	}

	if !a.Dev && !a.Password && len(a.Providers) == 0 {
		return errors.New("no sign-in method: enable ESA_AUTH_PASSWORD, or configure ESA_OIDC_* or ESA_GOOGLE_*")
	}
	return nil
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("want true or false, got %q", s)
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.ToLower(strings.TrimSpace(part)); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func isLoopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	return isLoopbackHost(host)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
