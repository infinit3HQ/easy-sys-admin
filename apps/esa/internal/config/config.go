// Package config reads control plane settings from the environment.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
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
	// channels point at private and loopback addresses (10/8, 172.16/12,
	// 192.168/16, 127/8, ...). Link-local and cloud metadata addresses are
	// always refused.
	// Defaults to true when signup is off, false when anyone can sign up.
	AllowPrivateNetworks bool

	// PollConcurrency is how many servers the minute poll contacts at once.
	PollConcurrency int
	// SSHConnectTimeout bounds the TCP connect to a managed server. The SSH
	// handshake after it has its own fixed limit.
	SSHConnectTimeout time.Duration
	// MaxServersPerAccount caps the servers one account can add.
	MaxServersPerAccount int
	// MaxOrgsPerUser caps organizations one user can own, not counting
	// their personal space.
	MaxOrgsPerUser int
	// BetaLimits is ESA_BETA_LIMITS. When set, each user may own one
	// organization besides their personal space, and each account may add
	// 10 servers. A self-hosted install leaves it off.
	BetaLimits bool
	// MetricsRawRetention is how long per-poll metric samples are kept.
	MetricsRawRetention time.Duration
	// MetricsHourlyRetention is how long hourly metric averages are kept.
	MetricsHourlyRetention time.Duration
	// Metrics serves GET /metrics in the Prometheus text format.
	Metrics bool
	// AdvisoryFeed is ESA_ADVISORY_FEED, a local file. Empty leaves matching off.
	AdvisoryFeed string

	Auth AuthConfig
	// SMTP sends confirmation and password-reset mail. Zero value means
	// those messages are off and password sign-up works without them.
	SMTP SMTPConfig
}

// SMTPConfig is the install's own mail account, not a notification channel.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// AuthConfig holds sign-in settings. Any combination of methods can be on.
type AuthConfig struct {
	// Dev signs everyone in as one local user. Only allowed on loopback.
	Dev bool
	// Password enables email and password sign-in.
	Password bool
	// Signup lets new people create an email and password account. It is
	// off when AllowedEmails is set, because a password sign-up cannot prove
	// the person owns the address. The first user can always sign up while
	// the database has no users.
	Signup bool
	// SignupIgnored is true when ESA_SIGNUP=true was turned off because
	// ESA_ALLOWED_EMAILS is set. The server logs a warning at startup.
	SignupIgnored bool
	// Providers are OpenID Connect providers (Authentik, Google, ...).
	Providers []OIDCProvider
	// AllowedEmails limits who may sign in with any method. Empty allows everyone.
	AllowedEmails []string
	// Mail is true when system SMTP is configured, so the sign-in page can
	// offer confirmation and password reset.
	Mail bool
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

	if err := parseLimits(&cfg, get); err != nil {
		return Config{}, err
	}
	if err := parseSMTP(&cfg, get); err != nil {
		return Config{}, err
	}
	cfg.Auth.Mail = cfg.SMTP.Host != ""

	return cfg, nil
}

// parseSMTP reads the system mail account. All of it is optional. Setting
// the host turns confirmation and password reset on, and then the from
// address is required. Port defaults to 587 (STARTTLS). Port 465 uses
// implicit TLS. The connection is refused when the server offers neither.
func parseSMTP(cfg *Config, get func(string, string) string) error {
	cfg.SMTP.Host = get("ESA_SMTP_HOST", "")
	if cfg.SMTP.Host == "" {
		return nil
	}
	var err error
	if cfg.SMTP.Port, err = parseInt(get("ESA_SMTP_PORT", "587"), 1, 65535); err != nil {
		return fmt.Errorf("ESA_SMTP_PORT: %w", err)
	}
	cfg.SMTP.Username = get("ESA_SMTP_USERNAME", "")
	cfg.SMTP.Password = get("ESA_SMTP_PASSWORD", "")
	if cfg.SMTP.Username != "" && cfg.SMTP.Password == "" {
		return errors.New("ESA_SMTP_PASSWORD is required when ESA_SMTP_USERNAME is set")
	}
	cfg.SMTP.From = get("ESA_SMTP_FROM", "")
	if cfg.SMTP.From == "" {
		return errors.New("ESA_SMTP_FROM is required when ESA_SMTP_HOST is set")
	}
	if _, err := mail.ParseAddress(cfg.SMTP.From); err != nil {
		return fmt.Errorf("ESA_SMTP_FROM: %w", err)
	}
	return nil
}

// parseLimits reads polling, retention, cap, and metrics settings.
func parseLimits(cfg *Config, get func(string, string) string) error {
	var err error
	if cfg.PollConcurrency, err = parseInt(get("ESA_POLL_CONCURRENCY", "64"), 1, 512); err != nil {
		return fmt.Errorf("ESA_POLL_CONCURRENCY: %w", err)
	}
	if cfg.SSHConnectTimeout, err = parseDuration(get("ESA_SSH_CONNECT_TIMEOUT", "5s"), time.Second, time.Minute); err != nil {
		return fmt.Errorf("ESA_SSH_CONNECT_TIMEOUT: %w", err)
	}
	if cfg.MaxServersPerAccount, err = parseInt(get("ESA_MAX_SERVERS_PER_ACCOUNT", "500"), 1, 100000); err != nil {
		return fmt.Errorf("ESA_MAX_SERVERS_PER_ACCOUNT: %w", err)
	}
	cfg.MaxOrgsPerUser = 20
	if cfg.BetaLimits, err = parseBool(get("ESA_BETA_LIMITS", "false")); err != nil {
		return fmt.Errorf("ESA_BETA_LIMITS: %w", err)
	}
	// The flag is the hosted beta. It replaces the server and organization
	// caps, including an explicit ESA_MAX_SERVERS_PER_ACCOUNT.
	if cfg.BetaLimits {
		cfg.MaxServersPerAccount = 10
		cfg.MaxOrgsPerUser = 1
	}
	if cfg.MetricsRawRetention, err = parseDuration(get("ESA_METRICS_RAW_RETENTION", "48h"), 2*time.Hour, 0); err != nil {
		return fmt.Errorf("ESA_METRICS_RAW_RETENTION: %w", err)
	}
	if cfg.MetricsHourlyRetention, err = parseDuration(get("ESA_METRICS_HOURLY_RETENTION", "2160h"), 2*time.Hour, 0); err != nil {
		return fmt.Errorf("ESA_METRICS_HOURLY_RETENTION: %w", err)
	}
	if cfg.MetricsHourlyRetention < cfg.MetricsRawRetention {
		return fmt.Errorf("ESA_METRICS_HOURLY_RETENTION (%s) must be at least ESA_METRICS_RAW_RETENTION (%s)", cfg.MetricsHourlyRetention, cfg.MetricsRawRetention)
	}
	if cfg.Metrics, err = parseBool(get("ESA_METRICS", "true")); err != nil {
		return fmt.Errorf("ESA_METRICS: %w", err)
	}
	cfg.AdvisoryFeed = get("ESA_ADVISORY_FEED", "")
	return nil
}

// parseInt reads a whole number between lo and hi.
func parseInt(s string, lo, hi int) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("want a whole number from %d to %d, got %q", lo, hi, s)
	}
	return n, nil
}

// parseDuration reads a Go duration such as 5s or 48h, at least lo and, when
// hi is not zero, at most hi.
func parseDuration(s string, lo, hi time.Duration) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("want a duration such as 5s, 30m, or 48h, got %q", s)
	}
	if d < lo || (hi > 0 && d > hi) {
		if hi > 0 {
			return 0, fmt.Errorf("want a duration from %s to %s, got %s", lo, hi, d)
		}
		return 0, fmt.Errorf("want a duration of at least %s, got %s", lo, d)
	}
	return d, nil
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
	if a.Signup, err = parseBool(get("ESA_SIGNUP", "false")); err != nil {
		return fmt.Errorf("ESA_SIGNUP: %w", err)
	}
	a.Signup = a.Signup && a.Password
	if a.Signup && len(a.AllowedEmails) > 0 {
		a.Signup, a.SignupIgnored = false, true
	}

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
