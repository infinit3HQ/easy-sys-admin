package config

import "testing"

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
