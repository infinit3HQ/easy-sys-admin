package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/config"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/server"
)

func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the control plane (API, sign-in, background jobs)",
		Long: `Run the control plane. Settings come from ESA_* environment variables
(docs/configuration.md lists them all):

  ESA_DATABASE_URL        PostgreSQL URL (required)
  ESA_MASTER_KEY          base64 key that seals stored secrets (required)
  ESA_LISTEN              bind address, default 127.0.0.1:8080
  ESA_PUBLIC_URL          origin users open, default http://localhost:3000
  ESA_HELPER_DIR          directory with esa-helper-linux-{amd64,arm64}, default ./bin
  ESA_AUTH_MODE           standard (default) or dev (loopback only)
  ESA_AUTH_PASSWORD       email and password sign-in, default true
  ESA_SIGNUP              open sign-up, default false
  ESA_OIDC_ISSUER         OIDC issuer URL, for example https://auth.example.com/application/o/esa/
  ESA_OIDC_CLIENT_ID      OIDC client id
  ESA_OIDC_CLIENT_SECRET  OIDC client secret
  ESA_OIDC_PROVIDER_NAME  name on the sign-in button, default Authentik
  ESA_GOOGLE_CLIENT_ID    Google sign-in client id
  ESA_GOOGLE_CLIENT_SECRET Google sign-in client secret
  ESA_ALLOWED_EMAILS      comma-separated allow list, empty allows everyone
  ESA_SMTP_HOST           system SMTP for confirmation and password reset, empty leaves them off
  ESA_ALLOW_PRIVATE_NETWORKS  allow private addresses; default true unless sign-up is open
  ESA_LOG_FORMAT          text (default) or json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.FromEnv()
			if err != nil {
				return err
			}
			return server.Run(cmd.Context(), cfg, newLogger(cfg.LogFormat))
		},
	}
}

// newHealthcheckCommand exists because the container image has no shell or
// curl. Docker's HEALTHCHECK runs `esa healthcheck` instead.
func newHealthcheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "healthcheck",
		Short:  "Exit 0 when the local control plane answers /healthz",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			listen := os.Getenv("ESA_LISTEN")
			if listen == "" {
				listen = "127.0.0.1:8080"
			}
			host, port, err := net.SplitHostPort(listen)
			if err != nil {
				return err
			}
			if host == "" || host == "0.0.0.0" || host == "::" {
				host = "127.0.0.1"
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/healthz", nil)
			if err != nil {
				return err
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("healthz returned %s", resp.Status)
			}
			return nil
		},
	}
}
