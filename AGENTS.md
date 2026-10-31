# AGENTS.md

Instructions for coding agents working in this repository. Read this file first, then the `AGENTS.md` of the package you change:

- [apps/esa/AGENTS.md](apps/esa/AGENTS.md): Go control plane, CLI and server helper
- [apps/web/AGENTS.md](apps/web/AGENTS.md): web app and MCP endpoint
- [packages/api/AGENTS.md](packages/api/AGENTS.md): the OpenAPI contract

Background is in [docs/](docs/README.md). Start with [docs/architecture.md](docs/architecture.md).

## What this is

easy-sys-admin manages a fleet of Linux servers. A Go control plane (`esa serve`) keeps state in PostgreSQL and reaches servers over SSH as a restricted `esa` user that can only run `esa-helper`. A TanStack Start web app serves the board and an MCP endpoint and proxies `/api`, `/auth` and `/enroll` to the control plane. All three front doors (web, CLI, MCP) go through one API described in `packages/api/openapi.yaml`.

## Layout

| Path           | Contents                                                                                  |
| -------------- | ----------------------------------------------------------------------------------------- |
| `apps/esa`     | Go module: `cmd/esa`, `cmd/esa-helper`, `internal/*` (business logic in `internal/fleet`) |
| `apps/web`     | Web app: `src/routes`, `src/components`, `src/lib`, `src/server` (proxy and MCP)          |
| `packages/api` | `openapi.yaml` and the generated TypeScript types                                         |
| `deploy`       | Compose stack for one Docker host behind Traefik                                          |
| `docs`         | Documentation. `docs/archive` holds old plans; do not treat them as current               |

## Setup and commands

Node 26, pnpm 12, Go 1.27, PostgreSQL. Copy `.env.example` to `.env` and set `ESA_MASTER_KEY`.

```sh
pnpm install
pnpm generate                                      # after changing openapi.yaml or routes
pnpm turbo run generate lint typecheck build test  # the full check; run it before you finish
pnpm --filter @esa/esa dev                         # control plane on 127.0.0.1:8080
pnpm --filter @esa/web dev                         # web on http://localhost:3000
```

Go tests that touch the database need `ESA_TEST_DATABASE_URL` pointing at a disposable database. The fleet tests drop and recreate its `public` schema. Never point it at a database with data you want to keep.

## Rules

- Change the API contract first. Edit `packages/api/openapi.yaml`, run `pnpm generate`, then implement. Never hand-edit `*.gen.go`, `*.gen.ts` or `apps/web/src/routeTree.gen.ts`. Commit generated files with the change.
- Every fleet operation checks permission with `a.can(role, scope)` and reads account data through `s.scoped` or the visibility helpers in `apps/esa/internal/fleet/sharing.go`. Owner-only writes stay owner-only. Write an audit entry for every change.
- The web UI hides controls a user cannot use, but the API is the only real check. Never move a permission decision into the UI.
- Change a managed server only through an `esa-helper` action. Keep the helper's safety rules (no volume or image pruning, protected units and containers, no shell). They are listed in `apps/esa/AGENTS.md` and tested in `apps/esa/internal/hostops/safety_test.go`.
- Never log, return or commit secrets: passwords, private keys, API tokens, enroll, invite and share link tokens, `ESA_MASTER_KEY`. Paths with tokens go through `api.RedactPath`.
- Every setting is an environment variable. Document a new one in `docs/configuration.md` and `.env.example`, and in `deploy/` and `.github/workflows/deploy.yml` if production needs it.
- Schema changes go in `apps/esa/internal/store/models*.go`. AutoMigrate only adds; drops, renames and backfills go in `postMigrate`.
- Use ESLint and Prettier for TypeScript (not Biome), `gofmt` and `go vet` for Go. Shared npm versions live in the catalog in `pnpm-workspace.yaml`.
- Check UI changes in a real browser at desktop and phone widths. There are no frontend unit tests.
- Keep changes scoped. Do not refactor or add features nobody asked for.

## Writing style

Docs, UI copy and commit messages use plain English: short sentences, sentence-case headings, active voice, real numbers and paths. No marketing words, no emojis, no em dashes. Commit subjects are short and imperative, for example `Add ESA_ALLOW_PRIVATE_NETWORKS setting`; web-only changes start with `web:`. The body says why.

## Keep docs current

When behaviour changes, update the page that describes it in the same change:

| You changed                      | Update                                        |
| -------------------------------- | --------------------------------------------- |
| An environment variable          | `docs/configuration.md`, `.env.example`       |
| Deploy, compose or workflows     | `docs/deployment.md`                          |
| A helper action or safety rule   | `docs/server-helper.md`, `apps/esa/AGENTS.md` |
| Roles, teams or sharing          | `docs/teams.md`, `docs/security.md`           |
| CLI commands                     | `docs/cli.md`                                 |
| MCP tools                        | `docs/mcp.md`                                 |
| A design choice                  | `docs/decisions.md`                           |
| A feature landed or a gap closed | `docs/status.md`                              |
