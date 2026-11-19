package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func base() map[string]string {
	return map[string]string{
		"ESA_DATABASE_URL": "postgres://localhost/esa",
		"ESA_MASTER_KEY":   "YWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE=",
		"ESA_AUTH_MODE":    "dev",
	}
}

func TestDevModeOnlyOnLoopback(t *testing.T) {
	m := base()
	if _, err := parse(env(m)); err != nil {
		t.Fatalf("dev mode on loopback: %v", err)
	}
	m["ESA_LISTEN"] = "0.0.0.0:8080"
	if _, err := parse(env(m)); err == nil {
		t.Fatal("dev mode on 0.0.0.0 must be refused")
	}
	m["ESA_LISTEN"] = "127.0.0.1:8080"
	m["ESA_PUBLIC_URL"] = "https://esa.example.com"
	if _, err := parse(env(m)); err == nil {
		t.Fatal("dev mode with a public URL must be refused")
	}
}

func TestRequired(t *testing.T) {
	for _, k := range []string{"ESA_DATABASE_URL", "ESA_MASTER_KEY"} {
		m := base()
		delete(m, k)
		if _, err := parse(env(m)); err == nil {
			t.Errorf("missing %s accepted", k)
		}
	}
	m := base()
	m["ESA_AUTH_MODE"] = "standard"
	m["ESA_AUTH_PASSWORD"] = "false"
	if _, err := parse(env(m)); err == nil {
		t.Error("no sign-in method accepted")
	}
	m["ESA_GOOGLE_CLIENT_ID"] = "id"
	if _, err := parse(env(m)); err == nil {
		t.Error("google without secret accepted")
	}
	m["ESA_GOOGLE_CLIENT_SECRET"] = "secret"
	cfg, err := parse(env(m))
	if err != nil || len(cfg.Auth.Providers) != 1 || cfg.Auth.Providers[0].Issuer != "https://accounts.google.com" {
		t.Errorf("google provider: %+v, %v", cfg.Auth.Providers, err)
	}
}

func TestSignupDefaults(t *testing.T) {
	m := base()
	cfg, err := parse(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Signup || !cfg.AllowPrivateNetworks {
		t.Errorf("default: signup %v, private networks %v; want false, true", cfg.Auth.Signup, cfg.AllowPrivateNetworks)
	}

	m["ESA_SIGNUP"] = "true"
	if cfg, err = parse(env(m)); err != nil {
		t.Fatal(err)
	}
	if !cfg.Auth.Signup || cfg.AllowPrivateNetworks {
		t.Errorf("ESA_SIGNUP=true: signup %v, private networks %v; want true, false", cfg.Auth.Signup, cfg.AllowPrivateNetworks)
	}

	// An allow list turns open password sign-up off.
	m["ESA_ALLOWED_EMAILS"] = "Ana@Example.com"
	if cfg, err = parse(env(m)); err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Signup || !cfg.Auth.SignupIgnored || !cfg.AllowPrivateNetworks {
		t.Errorf("allow list: %+v, private networks %v", cfg.Auth, cfg.AllowPrivateNetworks)
	}
}

func TestLimitDefaults(t *testing.T) {
	cfg, err := parse(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollConcurrency != 64 || cfg.SSHConnectTimeout != 5*time.Second || cfg.MaxServersPerAccount != 500 ||
		cfg.MaxOrgsPerUser != 20 || cfg.BetaLimits ||
		cfg.MetricsRawRetention != 48*time.Hour || cfg.MetricsHourlyRetention != 90*24*time.Hour || !cfg.Metrics {
		t.Errorf("defaults: %+v", cfg)
	}
}

func TestSMTPSettings(t *testing.T) {
	m := base()
	cfg, err := parse(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Mail || cfg.SMTP.Host != "" {
		t.Fatalf("mail on by default: %+v", cfg.SMTP)
	}
	m["ESA_SMTP_HOST"] = "smtp.example.com"
	m["ESA_SMTP_FROM"] = "Easy Sys Admin <mail@example.com>"
	m["ESA_SMTP_USERNAME"] = "mail@example.com"
	m["ESA_SMTP_PASSWORD"] = "secret"
	cfg, err = parse(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Auth.Mail || cfg.SMTP.Port != 587 || cfg.SMTP.From == "" {
		t.Fatalf("smtp: %+v mail %v", cfg.SMTP, cfg.Auth.Mail)
	}
	m["ESA_SMTP_FROM"] = ""
	if _, err := parse(env(m)); err == nil || !strings.Contains(err.Error(), "ESA_SMTP_FROM") {
		t.Fatalf("missing from: %v", err)
	}
}

func TestBetaLimits(t *testing.T) {
	m := base()
	m["ESA_BETA_LIMITS"] = "true"
	m["ESA_MAX_SERVERS_PER_ACCOUNT"] = "2000"
	cfg, err := parse(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.BetaLimits || cfg.MaxServersPerAccount != 10 || cfg.MaxOrgsPerUser != 1 {
		t.Errorf("beta: %+v", cfg)
	}
	m["ESA_BETA_LIMITS"] = "nope"
	if _, err := parse(env(m)); err == nil || !strings.Contains(err.Error(), "ESA_BETA_LIMITS") {
		t.Errorf("bad flag: %v", err)
	}
}

func TestLimitSettings(t *testing.T) {
	m := base()
	m["ESA_POLL_CONCURRENCY"] = "128"
	m["ESA_SSH_CONNECT_TIMEOUT"] = "3s"
	m["ESA_MAX_SERVERS_PER_ACCOUNT"] = "2000"
	m["ESA_METRICS_RAW_RETENTION"] = "72h"
	m["ESA_METRICS_HOURLY_RETENTION"] = "8760h"
	m["ESA_METRICS"] = "false"
	cfg, err := parse(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollConcurrency != 128 || cfg.SSHConnectTimeout != 3*time.Second || cfg.MaxServersPerAccount != 2000 ||
		cfg.MetricsRawRetention != 72*time.Hour || cfg.MetricsHourlyRetention != 8760*time.Hour || cfg.Metrics {
		t.Errorf("set: %+v", cfg)
	}

	bad := map[string]string{
		"ESA_POLL_CONCURRENCY":         "0",
		"ESA_SSH_CONNECT_TIMEOUT":      "500ms",
		"ESA_MAX_SERVERS_PER_ACCOUNT":  "100001",
		"ESA_METRICS_RAW_RETENTION":    "1h",
		"ESA_METRICS_HOURLY_RETENTION": "24h", // shorter than the default raw retention of 48h
		"ESA_METRICS":                  "maybe",
	}
	for k, v := range bad {
		m := base()
		m[k] = v
		if _, err := parse(env(m)); err == nil || !strings.Contains(err.Error(), k) {
			t.Errorf("%s=%s: got %v, want an error naming the variable", k, v, err)
		}
	}
	for _, v := range []string{"513", "ten", "-1"} {
		m := base()
		m["ESA_POLL_CONCURRENCY"] = v
		if _, err := parse(env(m)); err == nil {
			t.Errorf("ESA_POLL_CONCURRENCY=%s accepted", v)
		}
	}
	for _, v := range []string{"5", "2m", "x"} {
		m := base()
		m["ESA_SSH_CONNECT_TIMEOUT"] = v
		if _, err := parse(env(m)); err == nil {
			t.Errorf("ESA_SSH_CONNECT_TIMEOUT=%s accepted", v)
		}
	}
}

func TestReportRetentionIsExplicitAndAtLeastOneDay(t *testing.T) {
	m := base()
	cfg, err := parse(env(m))
	if err != nil || cfg.ReportRetention != 0 {
		t.Fatalf("default: %v", err)
	}
	m["ESA_REPORT_RETENTION"] = "2160h"
	cfg, err = parse(env(m))
	if err != nil || cfg.ReportRetention != 90*24*time.Hour {
		t.Fatalf("configured: %v", err)
	}
	m["ESA_REPORT_RETENTION"] = "1h"
	if _, err := parse(env(m)); err == nil {
		t.Fatal("accepted retention shorter than one day")
	}
}

// Private addresses are on by default only when nobody outside the operator
// can sign in. A provider with no allow list lets any account of that
// provider in, whatever ESA_SIGNUP says.
func TestPrivateNetworksDefault(t *testing.T) {
	standard := func(extra map[string]string) map[string]string {
		m := base()
		m["ESA_AUTH_MODE"] = "standard"
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	google := map[string]string{"ESA_GOOGLE_CLIENT_ID": "id", "ESA_GOOGLE_CLIENT_SECRET": "secret"}
	cases := []struct {
		name  string
		extra map[string]string
		want  bool
	}{
		{"password only, sign-up closed", nil, true},
		{"sign-up open", map[string]string{"ESA_SIGNUP": "true"}, false},
		{"google, no allow list", google, false},
		{"google with an allow list", map[string]string{
			"ESA_GOOGLE_CLIENT_ID": "id", "ESA_GOOGLE_CLIENT_SECRET": "secret", "ESA_ALLOWED_EMAILS": "me@example.com"}, true},
	}
	for _, c := range cases {
		cfg, err := parse(env(standard(c.extra)))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if cfg.AllowPrivateNetworks != c.want {
			t.Errorf("%s: AllowPrivateNetworks = %v, want %v", c.name, cfg.AllowPrivateNetworks, c.want)
		}
	}
	m := standard(google)
	m["ESA_ALLOW_PRIVATE_NETWORKS"] = "true"
	if cfg, err := parse(env(m)); err != nil || !cfg.AllowPrivateNetworks {
		t.Errorf("an explicit setting must win: %v %v", cfg.AllowPrivateNetworks, err)
	}
}
