# easy-sys-admin

One board for a whole server fleet. Add servers, put them in groups, and set when each group gets its disks cleaned and packages patched. The board shows live status. AI agents read the same data over MCP.

Everything is configured with environment variables. You never need to edit code to deploy your own copy.

## What is in the repo

| Path           | What it is                                                                                                                    |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `apps/esa`     | Go: the control plane (`esa serve`), the CLI (`esa ...`), and `esa-helper`, the small binary installed on each managed server |
| `apps/web`     | TanStack Start: landing page, the board, and the MCP endpoint at `/mcp`                                                       |
| `packages/api` | `openapi.yaml`, the API contract. Go handlers, the Go CLI client, and the TypeScript client are all generated from it         |
| `deploy/`      | `compose.yaml` for one Docker host behind Traefik                                                                             |

## Deploy

1. Copy `deploy/.env.example` to `deploy/.env` and fill it in. The table below lists every setting.
2. Point a hostname at the host. Traefik must already run there with a network named `proxy`, or set `TRAEFIK_NETWORK`.
3. `docker compose -f deploy/compose.yaml --env-file deploy/.env up -d --build`

No ports are published. Traefik reaches the web container over its network, and the web container talks to the control plane and PostgreSQL over a private network. One hostname serves the app, the API (`/api/v1`), MCP (`/mcp`), and the enroll scripts (`/enroll`).

### With GitHub Actions

`.github/workflows/deploy.yml` builds and starts the stack on a self-hosted runner labelled `esa-deploy`. It writes the env file from repository settings into the runner's temp directory and deletes it when the job ends.

- Secrets: `ESA_MASTER_KEY`, `POSTGRES_PASSWORD`, and optionally `ESA_OIDC_CLIENT_SECRET` and `ESA_GOOGLE_CLIENT_SECRET`.
- Variables: `ESA_HOST`, and optionally every other setting in the table below. Pushes to `main` deploy after CI passes once `DEPLOY_ENABLED` is `true`. "Run workflow" deploys on demand.

## Settings

| Variable                                           | Default             | Meaning                                                                                                                                                                                                                               |
| -------------------------------------------------- | ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ESA_HOST`                                         | required            | Public hostname, for example `esa.example.com`                                                                                                                                                                                        |
| `ESA_PUBLIC_URL`                                   | `https://$ESA_HOST` | Origin users open. The deploy workflow sets it from `ESA_HOST`                                                                                                                                                                        |
| `ESA_MASTER_KEY`                                   | required            | 32 random bytes, base64 (`openssl rand -base64 32`). Encrypts stored logins and channel credentials. Comma-separate several keys to rotate: the first seals new secrets. Back it up, because without it stored secrets cannot be read |
| `POSTGRES_PASSWORD`                                | required            | Password of the bundled PostgreSQL. It takes effect only when the database is first created                                                                                                                                           |
| `ESA_AUTH_PASSWORD`                                | `true`              | Email and password sign-in                                                                                                                                                                                                            |
| `ESA_SIGNUP`                                       | `true`              | Anyone can create an email and password account, and gets their own fleet                                                                                                                                                             |
| `ESA_ALLOWED_EMAILS`                               | empty               | Comma-separated emails allowed to sign in with any method. Empty allows everyone                                                                                                                                                      |
| `ESA_OIDC_ISSUER`                                  | empty               | OpenID Connect issuer, for example Authentik. Redirect URI: `https://$ESA_HOST/auth/callback/oidc`                                                                                                                                    |
| `ESA_OIDC_CLIENT_ID`, `ESA_OIDC_CLIENT_SECRET`     | empty               | OIDC client                                                                                                                                                                                                                           |
| `ESA_OIDC_PROVIDER_NAME`                           | `Authentik`         | Name on the sign-in button                                                                                                                                                                                                            |
| `ESA_GOOGLE_CLIENT_ID`, `ESA_GOOGLE_CLIENT_SECRET` | empty               | Google sign-in. Redirect URI: `https://$ESA_HOST/auth/callback/google`                                                                                                                                                                |
| `ESA_STACK_NAME`                                   | `esa`               | Compose project name                                                                                                                                                                                                                  |
| `ESA_ROUTER`                                       | `esa`               | Traefik router and service name. Change it if your Traefik already has a router with this name                                                                                                                                        |
| `TRAEFIK_NETWORK`                                  | `proxy`             | Docker network Traefik watches                                                                                                                                                                                                        |
| `TRAEFIK_ENTRYPOINT`                               | `websecure`         | Traefik entrypoint                                                                                                                                                                                                                    |
| `TRAEFIK_CERT_RESOLVER`                            | `cloudflare`        | Traefik certificate resolver                                                                                                                                                                                                          |

Control plane only, for running it outside compose: `ESA_DATABASE_URL`, `ESA_LISTEN` (default `127.0.0.1:8080`), `ESA_HELPER_DIR`, `ESA_LOG_FORMAT` (`text` or `json`), and `ESA_AUTH_MODE=dev` for a one-click local login, which is only allowed on loopback. The web server reads `ESA_API_URL`, the address of the control plane.

## CLI

```sh
esa login https://esa.example.com --token esa_...   # token from Settings > API tokens
esa status
esa servers show media-01
esa runs start clean --group production --wait
```

`ESA_URL` and `ESA_TOKEN` override the saved login, so one CLI can point at any install. Add `--json` for scripts.

## MCP

Point an MCP client at `https://esa.example.com/mcp` with an API token as the bearer token. The tools match the token's scopes. `read` gives status, updates, services, containers, and reports. `logs:read` adds `get_logs`, and `actions` adds `start_run`.

## Local development

PostgreSQL, Go 1.27, Node 26, and pnpm 12 are needed. Copy `.env.example` to `.env`, then:

```sh
pnpm install
pnpm --filter @esa/esa dev      # control plane on :8080
pnpm --filter @esa/web dev      # web on :3000
```
