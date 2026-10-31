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

// AuthMode selects how users sign in.
type AuthMode string

const (
	// AuthOIDC signs users in through an OpenID Connect provider such as Authentik.
	AuthOIDC AuthMode = "oidc"
	// AuthDev signs everyone in as one local user. Only allowed on a loopback address.
	AuthDev AuthMode = "dev"
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

	Auth AuthConfig
}

// AuthConfig holds sign-in settings.
type AuthConfig struct {
	Mode AuthMode
	// ProviderName is shown on the sign-in button.
	ProviderName string
	OIDC         OIDCConfig
	// AllowedEmails limits who may sign in. Empty means anyone the provider lets through.
	AllowedEmails []string
}

// OIDCConfig holds OpenID Connect client settings.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
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
			Mode:         AuthMode(get("ESA_AUTH_MODE", string(AuthOIDC))),
			ProviderName: get("ESA_OIDC_PROVIDER_NAME", "Authentik"),
			OIDC: OIDCConfig{
				Issuer:       get("ESA_OIDC_ISSUER", ""),
				ClientID:     get("ESA_OIDC_CLIENT_ID", ""),
				ClientSecret: get("ESA_OIDC_CLIENT_SECRET", ""),
			},
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

	switch cfg.Auth.Mode {
	case AuthOIDC:
		o := cfg.Auth.OIDC
		if o.Issuer == "" || o.ClientID == "" || o.ClientSecret == "" {
			return Config{}, errors.New("ESA_OIDC_ISSUER, ESA_OIDC_CLIENT_ID and ESA_OIDC_CLIENT_SECRET are required when ESA_AUTH_MODE=oidc")
		}
	case AuthDev:
		if !isLoopback(cfg.Listen) || !isLoopbackHost(publicURL.Hostname()) {
			return Config{}, errors.New("ESA_AUTH_MODE=dev is only allowed when ESA_LISTEN and ESA_PUBLIC_URL are loopback addresses")
		}
	default:
		return Config{}, fmt.Errorf("ESA_AUTH_MODE must be oidc or dev, got %q", cfg.Auth.Mode)
	}

	return cfg, nil
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
