# AGENTS.md: apps/esa

This Go module builds three things: the control plane (`esa serve`), the `esa` CLI, and `esa-helper`, the root helper on managed servers. HTTP handlers are thin. The `fleet` package holds every permission check, account boundary, and audit entry. The helper is the only code that changes a host. Read this file before you change anything here, and follow the rules exactly.

## Layout

- `cmd/esa`: entry point for `esa serve` and the CLI. Calls `cli.Execute`.
- `cmd/esa-helper`: entry point for the helper. Parses flags, maps actions to `hostops`, prints one envelope. `dispatch.go` is the SSH forced command.
- `internal/api`: serves `/api/v1`. `Handlers` implements the generated `StrictServerInterface` on top of `fleet`. `api.gen.go` is generated.
- `internal/apiclient`: typed API client for the CLI. `client.gen.go` is generated.
- `internal/auth`: password, OIDC, and dev sign-in, sessions, bearer tokens, rate limiter. Turns requests into `fleet.Actor`.
- `internal/cli`: the cobra command tree: `serve`, `healthcheck`, `login`, `logout`, `status`, `servers`, `groups`, `runs`, `alerts`.
- `internal/config`: reads and validates `ESA_*` environment variables.
- `internal/crypto`: AES-256-GCM for secrets at rest, keyed by `ESA_MASTER_KEY`, with key rotation.
- `internal/fleet`: the service layer. Accounts, teams, sharing, servers, groups, runs, schedules, alerts, certificates, tokens, audit.
- `internal/hostops`: code that runs inside `esa-helper` on managed servers. Fixed programs with typed arguments, no shell.
- `internal/hostops/proto`: JSON messages between control plane and helper. No dependencies, imported by both sides.
- `internal/netguard`: decides which addresses esa may connect to (SSH hosts, certificate checks, notification endpoints).
- `internal/notify`: sends alerts to ntfy, webhooks (generic, Discord, Slack), and email.
- `internal/password`: argon2id hashing.
- `internal/remote`: SSH to managed servers with a pinned host key, helper install, connection pool.
- `internal/server`: wires store, fleet, auth, API, and background jobs behind one HTTP server.
- `internal/store`: PostgreSQL through GORM. Models, migrations, queries. Only `fleet` and `auth` use it.
- `internal/version`: version and commit, set with `-ldflags`.

## Permissions

- Every exported fleet operation takes an `Actor` as its first argument after `ctx`.
- Check permission first: `if err := a.can(role, scope); err != nil { return ..., err }`. `can` checks the role and the token scope together. Browser sessions have `Scopes == nil`, which means no scope limit.
- `a.Role` is the organization role. For anything group- or server-scoped (runs, host actions, logs, update checks, group settings), use the effective role from `s.roleOn(ctx, a, groupID)` in `fleet/roles.go` and check it with `canAct(a, role, min, scope)`. A viewer is admin of the groups their teams hold. Check the scope first with `a.hasScope`, then load the row, then check the role on its group. In a loop, use one `s.roles(a)` so the team groups load once.
- Organization-level work stays on `a.can` with the organization role: adding, changing, moving, and deleting servers, host keys, helper reinstall, enrollment, creating and deleting groups, sharing, alert rules, channels, certificates, members, teams, and `admin` tokens.
- Pick the least role and scope that fit: `RoleViewer`/`ScopeRead` for reads, `RoleAdmin`/`ScopeLogs` for logs, `RoleAdmin`/`ScopeActions` for runs and host actions, `RoleAdmin`/`ScopeAdmin` for configuration. Owner-only work uses `RoleOwner`.
- Return `ErrForbidden`, `ErrNotFound`, `invalid(field, ...)`, `ConflictError`, or `UnavailableError`. `api/handlers.go` maps them to HTTP codes. Do not write HTTP responses from `fleet`.
- Never trust an id from the request on its own. Load the row through one of the helpers below.

## Account boundaries

- Start every read of an account-owned table from `s.scoped(ctx, a)` or from a visibility helper in `fleet/sharing.go`: `visibleServers`, `visibleGroups`, `visibleRuns`, `loadVisibleServer`, `loadVisibleGroup`.
- Do not write new visibility SQL. The fragments in `sharing.go` are the only place that decides what a share exposes.
- For an action on a server the actor may see through a share, use `operableServer` or `s.canOperateServer`. Both use the effective role on the server's group. A `view` share never acts. An `operate` share needs admin on the group (capped at admin) plus the scope.
- Keep owner-only writes on `s.scoped` or `loadServer`, so shared rows are not found. This covers server settings, delete, host key, helper reinstall, enrollment, group settings, and assigning a server to a group.
- A run's servers must all belong to one owning account. Never mix own and shared servers in one run.
- Add a test to `isolation_test.go` or `sharing_test.go` when you add a read or write path.

## Audit

- Write an audit entry for every change: `s.audit(ctx, q, a, "thing.verb", target, detail)`.
- When the change lands in another account through a share, use `s.auditOn(ctx, q, a, ownerID, ...)`. It writes to the owner and keeps a copy in the actor's account.
- Inside a transaction, pass its `*store.Queries` so the entry commits with the change.
- Keep `detail` small and free of secrets.

## Secrets

- Never log secrets, tokens, passwords, private keys, or full request bodies.
- Log paths through `api.RedactPath`. Enroll, invite, and share tokens sit in URL paths.
- Store tokens and link secrets as hashes (`randomToken`, `hashToken`). Store credentials sealed with `crypto`.
- Never return a saved password or private key from the API.

## API contract

- `internal/api/api.gen.go` and `internal/apiclient/client.gen.go` come from `packages/api/openapi.yaml`. Do not edit them.
- Change the contract in `openapi.yaml`, then run `pnpm generate` at the repo root (or `go generate ./...` here).
- `var _ StrictServerInterface = (*Handlers)(nil)` in `api/handlers.go` must keep compiling. Add the new handler method when you add an operation.
- CI fails when the generated files differ from a fresh `go generate`. Commit them.

## Schema

- Models live in `internal/store/models.go`, `models_fleet.go`, `models_ops.go`, and `models_teams.go`.
- Add new models to `Models()` in `models.go`.
- `AutoMigrate` runs on start and only adds tables, columns, indexes, and constraints. It never drops or renames.
- Put drops, renames, and data fixes in `postMigrate` in `store/migrate.go` as SQL. Each step must be safe to run on every start. Do not add a conversion for data that never shipped.

## Host changes

- Change a managed host only through an `esa-helper` action backed by `hostops`. The control plane never runs ad hoc commands over SSH.
- A new action needs: a case in `run` and the `usage` text in `cmd/esa-helper/main.go`, a timeout in `timeoutFor` if it is slow, a `hostops` method, and types in `hostops/proto`.
- Run programs through `Host.Run` with fixed names and typed, validated arguments. No shell, no free text in arguments. The helper sets `PATH` to `hostops.SafePath`.
- Return `RefusedError` for safety refusals, `BadRequestError` for bad input, `BusyError` when work is in progress.
- `proto.Version` is the helper protocol version, reported by `esa-helper version`. Nothing checks it yet, so a new action on an old helper fails with `unknown action` until the helper is reinstalled. Bump it only for incompatible changes.
- `ssh-dispatch` accepts only `sudo -n /usr/local/lib/esa/esa-helper <action> ...`. Do not widen it.

### Safety rules

These are enforced in `hostops` and pinned by `hostops/safety_test.go`. Do not weaken them, and add a test when you add an action that touches the same area.

- Cleanups never prune images, volumes, containers, networks, or the whole system. No `docker system prune`, no `image prune`, no `--volumes`.
- Build cache prune never uses `all=true`, and is skipped while a build runs.
- Cleanups never restart, stop, or kill Docker or containers.
- VS Code cleanup keeps the newest copy and any copy a running process uses, keeps staging folders younger than an hour, never follows symlinks, and checks every directory is owned by the home's owner.
- Protected units can be started and restarted but never stopped: `ssh`, `sshd`, `docker`, `containerd`, `systemd-journald`, `systemd-logind`, `dbus`, `systemd-resolved`.
- Network and firewall units (`networking`, `systemd-networkd`, `NetworkManager`, `ufw`, `nftables`, `iptables`, `ip6tables`, `netfilter-persistent`, `firewalld`) are never started, stopped, or restarted.
- Power, reboot, suspend, rescue, and emergency units are never started or restarted.
- Unit names must match `unitNameRe` (`.service` only). Reject anything with shell syntax.
- Template instances (`name@instance`) are started or restarted only when systemd already has them loaded.
- Protected containers (default prefixes `traefik`, `authentik`, `portainer`, `crowdsec`, or the server's own list) need `--confirm <exact name>` for actions and updates. Compose project and service names count as names.
- `container-update` refuses containers not managed by docker compose.
- `authorized_keys` reading skips FIFOs and symlinks.
- `/etc/shadow` is read only to classify a password as locked, empty, or set. The hash is not returned, logged, or stored.
- Sudoers reads are regular files under `/etc/sudoers` and `/etc/sudoers.d` only. No symlink, no FIFO, size capped, and `#include` paths outside that directory are not followed.
- A server whose host key changed gets no work until someone accepts the new key.
- Reboot runs started by a schedule skip the server that hosts the control plane. The reboot after an update always skips it.

## Tests

- Database tests need `ESA_TEST_DATABASE_URL` pointing at a disposable PostgreSQL database. Without it they skip.
- The fleet tests drop and recreate the `public` schema. The auth tests use their own `esa_auth_test` schema. Never point the variable at a database you care about.
- Run with `-p 1` locally so packages do not wipe each other's schema.
- `pnpm --filter @esa/esa test` loads the repo's `.env` before `go test`.

## Commands

Run from `apps/esa`:

```sh
go build ./...
go vet ./...
gofmt -l .                      # must print nothing
go test -p 1 -count=1 ./...     # with ESA_TEST_DATABASE_URL set
go generate ./...               # after openapi.yaml changes
```

From the repo root:

```sh
pnpm --filter @esa/esa build    # bin/esa plus bin/esa-helper-linux-{amd64,arm64}
pnpm --filter @esa/esa dev      # helpers, then esa serve with ../../.env
pnpm generate                   # Go and TypeScript code from openapi.yaml
```
