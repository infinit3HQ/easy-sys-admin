# Development

This page explains how to run easy-sys-admin on your own machine and how to change it. The repository is a pnpm workspace driven by Turborepo. The Go module in `apps/esa` holds the control plane, the CLI, and `esa-helper`. `apps/web` is the TanStack Start web app. `packages/api/openapi.yaml` is the API contract that the Go server, the Go CLI client, and the TypeScript client are generated from. The sections below cover local setup, the repository layout, every script, the full check, code generation, tests, adding an API endpoint, adding a helper action, and the conventions the code follows.

## Local setup

You need:

- PostgreSQL. Any recent version works, and Postgres.app works on macOS. Compose and CI use PostgreSQL 18.
- Go 1.27, the version in `apps/esa/go.mod`. `oapi-codegen` is a Go tool dependency of the module, so you do not install it separately.
- Node 26, from `.nvmrc`. `package.json` requires Node 26 or later.
- pnpm 12.15.0, from `packageManager` in `package.json`. Node 25 and later no longer ship corepack, so install it with `npm install -g pnpm@12.15.0`.

Then:

1. Create a database for the app and one for the tests:

   ```sh
   createdb esa_dev
   createdb esa_test
   ```

2. Copy the example env file:

   ```sh
   cp .env.example .env
   ```

3. Edit `.env`:
   - Set `ESA_MASTER_KEY` to the output of `openssl rand -base64 32`.
   - Check `ESA_DATABASE_URL`. The default, `postgres://localhost:5432/esa_dev?sslmode=disable`, connects as your own user, which is how Postgres.app is set up.
   - Uncomment `ESA_TEST_DATABASE_URL` to run the database tests.

4. Install dependencies:

   ```sh
   pnpm install
   ```

5. Start the control plane and the web server in two terminals:

   ```sh
   pnpm --filter @esa/esa dev   # control plane on 127.0.0.1:8080
   pnpm --filter @esa/web dev   # web on http://localhost:3000
   ```

   `pnpm dev` starts both through Turborepo in one terminal instead, after running `generate`.

6. Open `http://localhost:3000`.

The control plane's `dev` script first builds the helper binaries into `apps/esa/bin`, then runs `go run ./cmd/esa serve`. It migrates the database on start. Its address comes from `ESA_LISTEN`. The web dev server listens on `WEB_PORT`, default 3000, and does not read `PORT`. It exits instead of picking another port when 3000 is taken. If you change `WEB_PORT`, change `ESA_PUBLIC_URL` to match. The web server proxies `/api`, `/auth`, and `/enroll` to `ESA_API_URL`, default `http://127.0.0.1:8080`.

### Dev login

`.env.example` sets `ESA_AUTH_MODE=dev`. The login page then shows "Continue as the local dev user", which signs you in as "Dev Admin" (`dev@localhost`). Password sign-in stays on as well. The control plane only accepts `ESA_AUTH_MODE=dev` when `ESA_LISTEN` and `ESA_PUBLIC_URL` both use a loopback address or `localhost`, and the login only answers requests from loopback. See [configuration.md](configuration.md) for every setting.

## Repository layout

| Path                          | What it holds                                                                                                  |
| ----------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `apps/esa/cmd/esa`            | Entry point of `esa`: the control plane (`esa serve`) and the CLI                                              |
| `apps/esa/cmd/esa-helper`     | Entry point of `esa-helper`, the binary installed on each managed server. `main.go` maps actions to code.      |
| `apps/esa/internal/api`       | `/api/v1` handlers. `api.gen.go` is generated from the OpenAPI spec.                                           |
| `apps/esa/internal/apiclient` | Typed Go client for the CLI, generated into `client.gen.go`                                                    |
| `apps/esa/internal/auth`      | Sessions, OIDC and Google sign-in, password sign-in, the dev login, and bearer tokens                          |
| `apps/esa/internal/cli`       | CLI commands, `esa serve`, and `esa healthcheck`                                                               |
| `apps/esa/internal/config`    | Reads and validates the control plane's environment variables                                                  |
| `apps/esa/internal/crypto`    | Encrypts secrets at rest with `ESA_MASTER_KEY`                                                                 |
| `apps/esa/internal/fleet`     | Business logic: servers, groups, runs, schedules, alerts, teams, sharing, permission checks, and the audit log |
| `apps/esa/internal/hostops`   | What the helper does on a server. `proto` defines the JSON messages between the control plane and the helper.  |
| `apps/esa/internal/netguard`  | Decides which addresses esa may connect to                                                                     |
| `apps/esa/internal/notify`    | Sends alert messages to notification channels                                                                  |
| `apps/esa/internal/password`  | Password hashing with argon2id                                                                                 |
| `apps/esa/internal/remote`    | SSH connection pool and helper installation                                                                    |
| `apps/esa/internal/server`    | Wires everything into one HTTP server, with middleware and background jobs                                     |
| `apps/esa/internal/store`     | GORM models and migrations                                                                                     |
| `apps/esa/internal/version`   | Build version and commit, set with `-ldflags`                                                                  |
| `apps/esa/scripts`            | `build-helpers.mjs`, which cross-compiles the helper for linux/amd64 and linux/arm64                           |
| `apps/web/src/routes`         | Pages, plus server routes for `/healthz`, `/mcp`, and the `/api`, `/auth`, and `/enroll` proxies               |
| `apps/web/src/server`         | Server-only code: the control plane proxy and the MCP server                                                   |
| `apps/web/src/lib`            | API client wrapper (`api.ts`), queries (`queries.ts`), and formatting helpers                                  |
| `apps/web/src/components`     | React components. `ui` holds the shadcn/ui components.                                                         |
| `packages/api`                | `openapi.yaml` and the TypeScript client. `src/schema.gen.ts` is generated.                                    |
| `packages/eslint-config`      | Shared ESLint flat config                                                                                      |
| `packages/tsconfig`           | Shared TypeScript configs                                                                                      |
| `deploy`                      | `compose.yaml` and `.env.example` for one Docker host behind Traefik                                           |
| `docs`                        | Documentation                                                                                                  |
| `.github/workflows`           | `ci.yml` (checks) and `deploy.yml` (deploys on a self-hosted runner)                                           |
| `go.work`                     | Go workspace that uses `./apps/esa`, so Go tools work from the repository root                                 |

## Scripts

### Root

| Script              | Runs                  | What it does                                    |
| ------------------- | --------------------- | ----------------------------------------------- |
| `pnpm dev`          | `turbo run dev`       | Starts the control plane and the web dev server |
| `pnpm build`        | `turbo run build`     | Builds `esa`, the helpers, and the web server   |
| `pnpm generate`     | `turbo run generate`  | Regenerates every generated file                |
| `pnpm lint`         | `turbo run lint`      | Lints every package                             |
| `pnpm typecheck`    | `turbo run typecheck` | Type-checks the TypeScript packages             |
| `pnpm test`         | `turbo run test`      | Runs the Go tests                               |
| `pnpm format`       | `prettier --write .`  | Formats every file Prettier handles             |
| `pnpm format:check` | `prettier --check .`  | Fails when a file is not formatted              |

### `@esa/esa` (apps/esa)

`apps/esa/package.json` only lets pnpm and Turborepo run the Go tasks.

| Script          | What it does                                                                                                                              |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `generate`      | `go generate ./...`. Runs `oapi-codegen` to write `internal/api/api.gen.go` and `internal/apiclient/client.gen.go`.                       |
| `build`         | Builds `bin/esa`, then runs `build:helpers`                                                                                               |
| `build:helpers` | Builds `bin/esa-helper-linux-amd64` and `bin/esa-helper-linux-arm64` with CGO off. The version is `ESA_VERSION`, or `dev-<short commit>`. |
| `dev`           | Runs `build:helpers`, loads `../../.env`, then runs `serve`                                                                               |
| `serve`         | `go run ./cmd/esa serve`                                                                                                                  |
| `lint`          | `go vet ./...`, and fails when `gofmt -l .` lists a file                                                                                  |
| `test`          | Loads `../../.env`, then runs `test:go`                                                                                                   |
| `test:go`       | `go test ./...`                                                                                                                           |

### `@esa/web` (apps/web)

| Script      | What it does                                                                                               |
| ----------- | ---------------------------------------------------------------------------------------------------------- |
| `dev`       | Loads `../../.env` and starts the Vite dev server                                                          |
| `build`     | `vite build`. Writes the server bundle to `.output`.                                                       |
| `start`     | Loads `../../.env` and runs `.output/server/index.mjs`. `HOST` defaults to `127.0.0.1` and `PORT` to 3000. |
| `generate`  | `tsr generate`. Writes `src/routeTree.gen.ts` from the files in `src/routes`.                              |
| `lint`      | `eslint .`                                                                                                 |
| `typecheck` | `tsc`                                                                                                      |

### `@esa/api` (packages/api)

| Script      | What it does                                                 |
| ----------- | ------------------------------------------------------------ |
| `generate`  | `openapi-typescript openapi.yaml --output src/schema.gen.ts` |
| `lint`      | `redocly lint openapi.yaml`, then `eslint .`                 |
| `typecheck` | `tsc`                                                        |

`packages/eslint-config` and `packages/tsconfig` have no scripts.

### Turborepo tasks

`turbo.json` sets the order. `^task` means the same task in the packages this one depends on. `@esa/esa` and `@esa/web` both depend on `@esa/api`, so the API types are generated before anything else.

| Task        | Depends on              | Outputs                                   | Notes                          |
| ----------- | ----------------------- | ----------------------------------------- | ------------------------------ |
| `generate`  | `^generate`             | `src/**/*.gen.ts`, `internal/**/*.gen.go` |                                |
| `build`     | `^build`, `generate`    | `.output/**`, `bin/**`                    |                                |
| `lint`      | `generate`              |                                           |                                |
| `typecheck` | `^generate`, `generate` |                                           |                                |
| `test`      | `generate`              |                                           |                                |
| `dev`       | `generate`              |                                           | Not cached, runs until stopped |

Turborepo caches task results. A `test` run with unchanged inputs replays the cached result. Add `--force` to run it again. Turborepo also runs tasks in strict environment mode: shell variables that `turbo.json` does not list do not reach the tasks. The `dev`, `start`, and `test` scripts load `.env` themselves, so put local settings there.

## Full check

Run this before you push. It covers what CI checks, in one command:

```sh
pnpm turbo run generate lint typecheck build test
```

CI (`.github/workflows/ci.yml`) also runs the Go tests with the race detector, and fails when a regenerated file differs from the committed one. Run `git status` after the check to catch the second case.

## Code generation

The API contract lives in `packages/api/openapi.yaml`. To change the API, edit that file, then run:

```sh
pnpm generate
```

That writes:

- `apps/esa/internal/api/api.gen.go`, the Go server interface and types
- `apps/esa/internal/apiclient/client.gen.go`, the Go client the CLI uses
- `packages/api/src/schema.gen.ts`, the TypeScript types

`pnpm generate` also writes `apps/web/src/routeTree.gen.ts` from the route files. The Vite dev server keeps it up to date while it runs.

Never edit a `*.gen.go`, `*.gen.ts`, or `routeTree.gen.ts` file by hand. Generation overwrites it, and CI fails when the committed file does not match the generator. Commit the generated files together with the change that produced them.

## Tests

The Go tests run with `pnpm test`, or with `go test ./...` in `apps/esa`.

The tests that need a database read `ESA_TEST_DATABASE_URL` and are skipped when it is unset. Point it at a disposable database, never at one with data you want to keep:

- The fleet tests (`apps/esa/internal/fleet`) drop and recreate the `public` schema at the start of each test.
- The auth tests (`apps/esa/internal/auth`) use their own `esa_auth_test` schema, so they can run at the same time as the fleet tests.

`go test` runs packages in parallel. If you add another package whose tests reset the `public` schema, or if you share one database between several packages that reset it, run `go test -p 1 ./...` so packages run one at a time. Do not run two test runs against the same database at once. CI runs `go test -race -count=1 ./...` against a fresh PostgreSQL 18 service.

There are no frontend unit tests. Check UI changes in a browser against the dev servers, and check `pnpm lint` and `pnpm typecheck`.

## Add an API endpoint

1. Describe the endpoint in `packages/api/openapi.yaml`: path, method, `operationId`, parameters, request body, responses, and schemas. Run `pnpm --filter @esa/api lint` to check the spec.
2. Run `pnpm generate`. The Go build now fails, because `Handlers` in `apps/esa/internal/api/handlers.go` must implement the new method of `StrictServerInterface`.
3. Write the handler in `apps/esa/internal/api`, in the file for that area (for example `live.go` or `servers.go`). The method is named after the `operationId`. Get the caller with `actor(ctx)`, call the fleet service, and return the generated response type, for example `ListServices200JSONResponse`. Return fleet errors unchanged. `errorStatus` maps them to HTTP status codes. Put conversions between fleet and API types in `convert.go`.
4. Write the logic in `apps/esa/internal/fleet` as a method on `*Service` that takes the `Actor`:
   - Check permissions first with `a.can(role, scope)`, for example `a.can(RoleViewer, ScopeRead)` for reads and `a.can(RoleAdmin, ScopeAdmin)` for changes. The role is the caller's role in the account. The scope limits API tokens. Browser sessions have no scope limit.
   - Load only rows the caller may see, for example with `loadVisibleServer` or `operableServer`.
   - Return `ValidationError`, `ErrNotFound`, `ErrForbidden`, or `ConflictError` for expected failures.
   - Record every change in the audit log with `s.audit(ctx, nil, a, "thing.verb", target, detail)`, or `s.auditOn` when the change touches a server another organization owns.
5. Add a test in the fleet package. Use `newTestService(t)` and `signIn(t, s, "name")`. Check that a viewer cannot change anything and that another account cannot see the data, as `isolation_test.go` does.
6. Use the endpoint in the web app. Add a query to `q` in `apps/web/src/lib/queries.ts` with `queryOptions` and `call(api().GET('/path', { params }))`. For changes, use `useMutation` in the component with `call(api().POST(...))` and invalidate the affected queries. Export a named type from `packages/api/src/index.ts` if components need the schema.
7. Run the [full check](#full-check).

## Add a helper action

The control plane calls the helper over SSH as `sudo -n /usr/local/lib/esa/esa-helper <action> [flags]`. The helper prints one JSON envelope on stdout.

1. Write the operation in `apps/esa/internal/hostops` as a method on `*Host`. Run programs through the `Runner` in `h.Run` (or the `h.output` helper) and read files through `h.readFile`, so tests can swap in a fake runner and a fixture tree. Return `BadRequestError` for invalid arguments, `RefusedError` when a safety rule blocks the action, and `BusyError` when another process holds a lock.
2. Add the response type to `apps/esa/internal/hostops/proto/ops.go`, with camelCase JSON tags. The `proto` package has no dependencies, because both the control plane and the helper import it.
3. Dispatch the action in `run` in `apps/esa/cmd/esa-helper/main.go`: add a `case`, add any flags to the flag set, add the action to the usage text, and add it to `timeoutFor` if it needs more than the default 2 minutes. The SSH forced command (`cmd/esa-helper/dispatch.go` and `hostops.DispatchArgs`) passes any action through sudo, so it needs no change.
4. Call the action from `apps/esa/internal/fleet/hostcalls.go`. Check `a.can(role, scope)` first. Use `s.call` for reads on any server the caller can see. For actions that change a server, get the server with `operableServer` and use `s.callServer`, then audit the action.
5. Add tests in `hostops` for parsing and safety rules, like `parse_test.go` and `safety_test.go`.
6. Rebuild the helpers with `pnpm --filter @esa/esa build:helpers`. Servers keep the helper they have until it is reinstalled, so an older helper answers a new action with `unknown action`. Change `proto.Version` only for incompatible changes. Nothing checks it yet, so reinstall the helper on servers that need the new action.

Then expose the action through an API endpoint as described above.

## Conventions

- TypeScript is linted with ESLint, using the shared flat config in `packages/eslint-config`, which builds on `@tanstack/eslint-config` and has no formatting rules. Prettier formats the code (`prettier.config.js`: no semicolons, single quotes, trailing commas). The repository does not use Biome. `.prettierignore` skips `apps/esa`, the lockfile, and generated files.
- Go code is formatted with `gofmt` and checked with `go vet`. `pnpm --filter @esa/esa lint` runs both.
- `.editorconfig` sets two-space indentation, with tabs for Go.
- Shared dependency versions live in the `catalog` in `pnpm-workspace.yaml`. Packages reference them as `"catalog:"`, and workspace packages as `"workspace:*"`. To add a dependency that more than one package uses, add it to the catalog first.
- Only `esbuild` and `lightningcss` may run install scripts (`allowBuilds` in `pnpm-workspace.yaml`). pnpm blocks the rest.
- pnpm 11 and later refuse to install a package version published less than a day ago (`minimumReleaseAge`, 1440 minutes by default). The repository keeps that default. It protects against hijacked releases, which are usually found and pulled within hours. When a dependency's newest release is younger than a day, `pnpm install` fails to resolve it. The `overrides` in `pnpm-workspace.yaml` pin `electron-to-chromium`, `node-releases`, `seroval`, `tslib`, and `ws` to releases that had passed that window. You can raise or drop a pin once the newer release is more than a day old.
- Commit subjects are short, in the imperative, in sentence case, and without a trailing period, for example `Add ESA_ALLOW_PRIVATE_NETWORKS setting`. Changes that only touch the web app start with `web:`, for example `web: scan only src for Tailwind classes`. The body explains why the change was made. Larger changes list their parts as bullets. Run `git log --oneline -15` to see recent examples.
