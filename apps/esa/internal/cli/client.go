package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/apiclient"
)

// cliConfig is stored at ~/.config/esa/config.json with mode 0600.
type cliConfig struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		dir = xdg
	} else if home, err := os.UserHomeDir(); err == nil {
		dir = filepath.Join(home, ".config") // same place on macOS and Linux
	}
	return filepath.Join(dir, "esa", "config.json"), nil
}

func loadConfig() (cliConfig, error) {
	var c cliConfig
	p, err := configPath()
	if err != nil {
		return c, err
	}
	if raw, err := os.ReadFile(p); err == nil {
		if err := json.Unmarshal(raw, &c); err != nil {
			return c, fmt.Errorf("read %s: %w", p, err)
		}
	}
	// Environment variables win, for CI and scripts.
	if v := os.Getenv("ESA_URL"); v != "" {
		c.URL = v
	}
	if v := os.Getenv("ESA_TOKEN"); v != "" {
		c.Token = v
	}
	if c.URL == "" || c.Token == "" {
		return c, errors.New("not logged in: run `esa login <url> --token esa_...` (create a token in Settings > API tokens)")
	}
	return c, nil
}

func saveConfig(c cliConfig) (string, error) {
	p, err := configPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	raw, _ := json.MarshalIndent(c, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return "", err
	}
	return p, os.Rename(tmp, p)
}

func normalizeURL(raw string) (string, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("%q is not a URL", raw)
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
		return "", errors.New("use https: the token would be sent in plain text")
	}
	return u.Scheme + "://" + u.Host, nil
}

func newClient(c cliConfig) (*apiclient.ClientWithResponses, error) {
	return apiclient.NewClientWithResponses(strings.TrimRight(c.URL, "/")+"/api/v1",
		apiclient.WithHTTPClient(&http.Client{Timeout: 10 * time.Minute}),
		apiclient.WithRequestEditorFn(func(_ context.Context, r *http.Request) error {
			r.Header.Set("Authorization", "Bearer "+c.Token)
			r.Header.Set("User-Agent", "esa-cli")
			return nil
		}))
}

// client loads the saved login and returns an API client.
func client() (*apiclient.ClientWithResponses, error) {
	c, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return newClient(c)
}

// apiErr turns a non-2xx response into an error with the API's message.
func apiErr(resp *http.Response, body []byte) error {
	if resp == nil {
		return errors.New("no response from the control plane")
	}
	if resp.StatusCode < 300 {
		return nil
	}
	var e apiclient.Error
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		if e.Error.Field != nil && *e.Error.Field != "" {
			return fmt.Errorf("%s: %s", *e.Error.Field, e.Error.Message)
		}
		return errors.New(e.Error.Message)
	}
	return fmt.Errorf("control plane answered %s", resp.Status)
}
