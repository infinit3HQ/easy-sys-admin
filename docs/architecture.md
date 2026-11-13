# Architecture

This document describes the parts of easy-sys-admin and how they fit together. It covers the request path from a browser, the CLI, or an MCP client to the Go control plane, the Go packages and binaries in `apps/esa`, the background jobs, the data model, code generation, how maintenance runs work, and how alerts reach notification channels. What runs on a managed server is in [server-helper.md](server-helper.md). Spaces, teams, and group sharing are in [teams.md](teams.md). The security model is in [security.md](security.md).

## Components

| Component     | Where                       | What it does                                                                                                       |
| ------------- | --------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| Web app       | `apps/web`                  | TanStack Start app. Renders the board, serves `/mcp`, and proxies `/api`, `/auth`, and `/enroll`.                  |
| Control plane | `apps/esa`, `esa serve`     | Go HTTP server. Owns the API, sign-in, background jobs, and all SSH connections to servers.                        |
| CLI           | `apps/esa`, `esa <command>` | Talks to `/api/v1` with an API token.                                                                              |
| Server helper | `apps/esa/cmd/esa-helper`   | Small binary on each managed server. The control plane runs it over SSH. See [server-helper.md](server-helper.md). |
| API contract  | `packages/api/openapi.yaml` | The single description of `/api/v1`. Go and TypeScript code is generated from it.                                  |
| Database      | PostgreSQL                  | All state. Only the control plane connects to it.                                                                  |
| Deploy        | `deploy/compose.yaml`       | PostgreSQL, the control plane, and the web app on one Docker host behind Traefik.                                  |

In the compose deploy only the `web` container is published, through Traefik. The control plane sits on an internal network with the web app. PostgreSQL sits on a separate network marked `internal: true` that only the control plane joins.

```
  browser ─┐                       ┌──────────── web (apps/web, port 3000) ─────────────┐
  CLI ─────┼── HTTPS ── Traefik ──>│ pages (SSR)    /mcp (mcp.ts)    /api /auth /enroll │
  MCP ─────┘                       └───────┬──────────────┬─────────────────┬───────────┘
                                           │ cookie       │ bearer token    │ proxy.ts
                                           v              v                 v
                                   ┌──────────── esa serve (apps/esa, port 8080) ───────┐
                                   │ api  auth  fleet  background jobs                  │──> PostgreSQL
                                   └───────────────────────┬────────────────────────────┘
                                                           │ SSH as "esa", pinned host key
                                                           v
                                   managed server: sshd -> esa-helper ssh-dispatch
                                                   -> sudo -n esa-helper <action>
```

## Request flow

### Browser and CLI requests

The browser, the CLI, and enroll scripts all use one origin. The web app routes `apps/web/src/routes/api/$.ts`, `auth/$.ts`, and `enroll/$.ts` hand every request to `proxyToControlPlane` in `apps/web/src/server/proxy.ts`. The proxy:

1. Refuses a body whose `Content-Length` is over 1 MiB with 413.
2. Drops hop-by-hop headers and the headers a client must not set: `x-real-ip`, `forwarded`, `x-esa-client`, `x-forwarded-host`, and `x-forwarded-proto`. It keeps `x-forwarded-for` as received, because Traefik appends the real client address and the control plane reads the rightmost entry.
3. Sets `x-forwarded-host` and `x-forwarded-proto` from the incoming request.
4. Forwards to `ESA_API_URL` (default `http://127.0.0.1:8080`) with `redirect: 'manual'`.
5. Uses a 60 second timeout for most calls. Calls under `/api/v1/servers`, `/api/v1/certs`, and `/enroll/` get 11 minutes, and they keep running if the browser tab closes, so a helper install is never cut off halfway.
6. Copies `Set-Cookie` headers one by one, so several cookies survive.

When a page renders on the server, `apps/web/src/lib/api.ts` calls the control plane directly at `ESA_API_URL` and forwards the visitor's cookie. In the browser it calls the same origin, which goes through the proxy.

In the control plane, `apps/esa/internal/server/server.go` wraps the mux in this middleware chain, outermost first: panic recovery, request logging, security headers, body limits, cross-origin protection, bearer token authentication, then session cookies. The mux serves `/healthz`, `/auth/...`, `/api/v1/...`, and `/enroll/...`.

### MCP requests

`/mcp` is handled in the web server by `apps/web/src/server/mcp.ts`. It does not go through the proxy. For each request it:

1. Rejects a request whose `Origin` header is set and differs from the origin of `ESA_PUBLIC_URL`.
2. Reads the `Authorization: Bearer esa_...` header and calls `GET /api/v1/me` on the control plane with that token. An unknown, expired, or revoked token gets 401.
3. Builds an MCP server whose tools call the control plane API directly at `ESA_API_URL`, with the caller's own token and `x-esa-client: mcp`.

So an agent can do exactly what its token's scopes and its user's role allow, and its calls appear in the audit log with source `mcp`. The proxy strips `x-esa-client` from browser and CLI requests, so only the MCP server can set that label. Which tools a token sees is described in [security.md](security.md#mcp).

## Binaries

| Binary       | Source                    | Notes                                                                                                                                                                              |
| ------------ | ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `esa`        | `apps/esa/cmd/esa`        | `esa serve` runs the control plane. `esa healthcheck` checks the local `/healthz`. Other commands are the CLI: `login`, `logout`, `status`, `servers`, `groups`, `runs`, `alerts`. |
| `esa-helper` | `apps/esa/cmd/esa-helper` | Built for `linux/amd64` and `linux/arm64` by `apps/esa/scripts/build-helpers.mjs` into `bin/esa-helper-linux-<arch>`. The control plane serves these files from `ESA_HELPER_DIR`.  |

The CLI stores the URL and token in `~/.config/esa/config.json` with mode 0600. It refuses plain `http` URLs except for `localhost` and `127.0.0.1`. In the Docker image the helpers live in `/usr/local/share/esa/helpers`.

## Go packages

All packages are under `apps/esa/internal`.

| Package         | Purpose                                                                                                            |
| --------------- | ------------------------------------------------------------------------------------------------------------------ |
| `api`           | Serves `/api/v1` through handlers generated from the OpenAPI file, plus the public `/enroll/{token}` endpoints.    |
| `apiclient`     | Typed API client for the CLI, generated from the OpenAPI file.                                                     |
| `auth`          | Sign-in (password, OIDC, Google, dev mode), session cookies, bearer tokens, and sign-in rate limits.               |
| `cli`           | The `esa` command tree, including `serve`.                                                                         |
| `config`        | Reads and validates `ESA_*` environment variables.                                                                 |
| `crypto`        | `Box`, which seals secrets at rest with AES-256-GCM under `ESA_MASTER_KEY`.                                        |
| `fleet`         | The service layer. Every front door goes through it, so permission checks and account isolation live in one place. |
| `hostops`       | The code that runs on managed servers inside `esa-helper`. Fixed programs with typed arguments, no shell.          |
| `hostops/proto` | JSON messages between the control plane and the helper. It has no dependencies, so both sides import it.           |
| `netguard`      | Decides which addresses esa may connect to for SSH, certificate checks, and notifications.                         |
| `notify`        | Delivers alert messages to ntfy, webhooks, Discord, Slack, and email.                                              |
| `password`      | Hashes and checks passwords with argon2id.                                                                         |
| `remote`        | SSH to managed servers with a pinned host key, the connection pool, and the helper install script.                 |
| `server`        | Wires store, fleet, auth, API, middleware, and background jobs into one HTTP server.                               |
| `store`         | PostgreSQL through GORM. The models define the schema, applied with AutoMigrate at startup.                        |
| `version`       | Build version and commit, set with `-ldflags`.                                                                     |

## Background jobs

`startJobs` in `apps/esa/internal/server/jobs.go` first calls `ResumeRuns`, then starts the loops below. Each loop waits for its first delay, then runs on its interval. Each run gets a deadline of the interval minus one twelfth, so a stuck run cannot hold up the next one. A panic in a run is logged and the loop goes on.

| Job      | Interval | First run after | What it does                                                                                                                                                                                                                                                                                                                                                           |
| -------- | -------- | --------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Resume   | 1 minute | 20 seconds      | `fleet.ResumeRuns`: also once at startup. Finishes a run whose worker no longer holds it. An upgrade is followed. Any other run is marked interrupted. A run that is still held is left alone. |
| Poll     | 1 minute | 5 seconds       | `fleet.PollAll`: calls `esa-helper status` on every ready server, `ESA_POLL_CONCURRENCY` (default 64) at a time, 45 seconds each. Stores the snapshot and a metric sample. When the exposure summary is older than an hour, it also calls `access` and `security`, at most 8 servers per cycle, then evaluates alerts. One process holds the poll for 70 seconds and renews it each cycle. Another process skips the cycle until that hold expires. |
| Schedule | 1 minute | 15 seconds      | `fleet.Tick`: starts group clean and update runs whose schedule slot is due.                                                                                                                                                                                                                                                                                           |
| Rollup   | 1 hour   | 2 minutes       | `fleet.Rollup`: averages raw samples into hourly rows, deletes samples older than `ESA_METRICS_RAW_RETENTION` (48 hours) and hourly rows older than `ESA_METRICS_HOURLY_RETENTION` (90 days).                                                                                                                                                                          |
| Sessions | 1 hour   | 1 minute        | `auth.Sessions.CleanupExpired`: deletes expired sessions and sessions idle for 24 hours.                                                                                                                                                                                                                                                                               |
| Deliver  | 1 minute | 10 seconds      | `fleet.DrainDeliveries`: sends notifications stored in the database, retries a delivery left in sending, and returns expired snoozes to the review list. Five failures mark a delivery dead.                                                                                                                                                                           |
| Digest   | 1 hour   | 3 minutes       | `fleet.SendDigests`: on Monday after 08:00 UTC, sends one summary per space to its channels. Unreachable servers, security updates waiting 7 days, and security notices that are still open. A repeated subject is listed once. An empty week sends nothing. A control plane that is down on Monday skips that week. A second run the same Monday does not send again. |
| Certs    | 12 hours | 3 minutes       | `fleet.CheckCerts`: connects to each certificate target and updates expiry alerts.                                                                                                                                                                                                                                                                                     |
| Audit    | 1 hour   | 4 minutes       | `fleet.PruneAudit`: deletes audit rows older than `ESA_AUDIT_RETENTION`. Unset does nothing. The ledger and the prune entry are written after the delete.                                                                                                                                                                                                              |

Polling details from `apps/esa/internal/fleet/poll.go`:

- Servers attempted longest ago go first, so a cycle cut short does not starve the same servers.
- A cycle may run for 55 seconds. One status call takes about 1.25 seconds on a real server, most of it a 1 second CPU sample, so the cycle reaches about 44 servers per worker. When the deadline arrives before every server has started, the cycle logs `poll cycle ran out of time` at warning level with the number skipped, the number of ready servers, and the concurrency. The skipped servers go first in the next cycle.
- A server is marked `unreachable` after 3 failed polls in a row. From then on it is polled once every 5 cycles (5 minutes) until it answers. A successful poll, accepting a new host key, reinstalling the helper, or deleting the server ends this back-off. The unreachable alert (3 minutes by default) still fires and resolves on the first poll after its duration. With the 5 minute spacing it fires about 5 minutes after the server is marked unreachable instead of 3, and resolves up to 5 minutes after the server answers again.
- Each SSH dial allows `ESA_SSH_CONNECT_TIMEOUT` (5 seconds) for the TCP connect and 15 seconds for the SSH handshake. The poll's context bounds both, so a cycle deadline also ends them.
- The pending update list is refreshed from apt's existing lists when it is older than 6 hours. It does not run `apt-get update`. A list with no successful check for 12 hours is marked stale, as is a server that has never been checked. Key and access facts use the exposure summary, which is stale after 2 hours.
- Servers that keep refusing esa's SSH login are polled less often: no delay for the first two failures, then 2, 4, 8, and 16 minutes, capped at 30. Such a server also counts as unreachable after 3 failures, and the longer of the two delays applies.
- A server whose host key changed is not contacted until a person accepts the new key.
- Back-off state is stored on the server row. A restarted control plane does not immediately retry a login that just failed. Rate limits stay in memory.
- The control plane serves `GET /metrics` with the last cycle's duration, server count, and skipped count, poll results and durations, pool size, running runs, and the notification queue. See [deployment.md](deployment.md#monitoring).

The schedule tick claims each slot with a conditional update, so two ticks or two control plane processes never start the same slot twice. A slot more than one hour late, for example after the control plane was down, is recorded as a `missed` run instead of running late.

## Data model

The schema is the set of structs in `apps/esa/internal/store/models*.go`. `store.Migrate` applies them with GORM AutoMigrate under a PostgreSQL advisory lock. AutoMigrate adds tables, columns, indexes, and constraints, and never drops or renames anything.

### Accounts and spaces

An account is a space that owns a fleet. Every server, group, run, alert, channel, certificate, token, and SSH key belongs to one account. There are two kinds:

- A personal space. Every user gets exactly one at first sign-in. `accounts.personal_of` holds the user id. It has no other members.
- An organization. `accounts.personal_of` is NULL. Any user can create one and invite others with the roles `owner`, `admin`, or `viewer`. New members join as viewers. Extra teams (`teams`, `team_members`, `team_groups`) grant admin rights on chosen groups and own nothing. The admin and viewer role names live on the account (`admin_team_name`, `viewer_team_name`).

A session works in one account at a time. An account can share a group, and the servers in it, with another organization at `view` or `operate` access. A personal space can share out and cannot receive. The full rules are in [teams.md](teams.md).

### Tables

Identity and access:

| Table         | Holds                                                                                                                                                |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `accounts`    | Personal spaces and organizations. `admin_team_name` and `viewer_team_name` are the role labels.                                                     |
| `users`       | People. The first sign-in stores its issuer and subject. `user_identities` records later providers. The same verified email joins the existing user. |
| `memberships` | A user's role (`owner`, `admin`, `viewer`) in an account.                                                                                            |
| `sessions`    | Signed-in browsers. Stores the SHA-256 of the cookie token only.                                                                                     |
| `api_tokens`  | CLI and MCP tokens. Stores the SHA-256 of the token, its scopes, and its expiry.                                                                     |
| `audit_log`   | Who did what, in which account, from which source (`web`, `api`, `mcp`, `system`).                                                                   |

Teams and sharing:

| Table          | Holds                                                                                 |
| -------------- | ------------------------------------------------------------------------------------- |
| `invites`      | One-time links that add a user to an organization. Token stored as SHA-256.           |
| `share_links`  | One-time links that share a group with another organization. Token stored as SHA-256. |
| `group_shares` | A group shared with a receiving account, with `view` or `operate` access.             |
| `teams`        | Extra teams of an organization. They own no servers.                                  |
| `team_members` | Which organization members are in an extra team.                                      |
| `team_groups`  | Server groups an extra team holds, including groups shared in.                        |

Fleet:

| Table                | Holds                                                                                                                      |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `account_keys`       | The account's ed25519 SSH key pair. The private key is sealed with `crypto.Box`.                                           |
| `groups`             | Sets of servers with cleanup and update settings and schedules, plus the last slot each schedule claimed.                  |
| `servers`            | Managed machines: address, pinned host key, enrollment state (`pending` or `ready`), helper version, protected containers. |
| `server_credentials` | A sudo login kept for helper reinstalls, only when the owner chose to keep it. Sealed.                                     |
| `enroll_tokens`      | One-time enroll commands for path A, valid 15 minutes. Token stored as SHA-256.                                            |
| `server_states`      | One row per server: status, reasons, latest snapshot, pending updates, exposure summary, missed polls, host key mismatch.  |
| `metric_samples`     | One row per poll, kept 48 hours.                                                                                           |
| `metric_hourly`      | Hourly averages (disk is the hourly maximum), kept 90 days.                                                                |

Operations:

| Table             | Holds                                                                           |
| ----------------- | ------------------------------------------------------------------------------- |
| `runs`            | One maintenance job: kind, trigger, status, options, summary, who requested it. |
| `run_servers`     | One server's part of a run: status, error, bytes freed, and a JSON detail.      |
| `alert_rules`     | One rule per alert kind per account, with enabled, threshold, and duration.     |
| `alerts`          | Pending, firing, and resolved alerts.                                           |
| `notify_channels` | Where alerts go. The configuration (URLs, tokens, SMTP settings) is sealed.     |
| `cert_targets`    | TLS endpoints whose certificate expiry esa checks, with the last result.        |

## Code generation

Run `pnpm generate` at the repository root. Turborepo also runs `generate` before `build`, `lint`, `typecheck`, `test`, and `dev`.

| Source                      | Tool                                                    | Output                                      |
| --------------------------- | ------------------------------------------------------- | ------------------------------------------- |
| `packages/api/openapi.yaml` | `oapi-codegen` (models, std-http-server, strict-server) | `apps/esa/internal/api/api.gen.go`          |
| `packages/api/openapi.yaml` | `oapi-codegen` (models, client)                         | `apps/esa/internal/apiclient/client.gen.go` |
| `packages/api/openapi.yaml` | `openapi-typescript`                                    | `packages/api/src/schema.gen.ts`            |
| `apps/web/src/routes/**`    | TanStack Router `tsr generate`                          | `apps/web/src/routeTree.gen.ts`             |

The Go generators run through `go generate ./...` from `//go:generate` lines in `internal/api/generate.go` and `internal/apiclient/generate.go`. The web app and the MCP server use `createApiClient` from `packages/api/src/index.ts`, an `openapi-fetch` client typed by `schema.gen.ts` that prefixes every path with `/api/v1`. `pnpm lint` in `packages/api` runs `redocly lint` on the contract.

## How runs work

A run is one maintenance job across one or more servers. Runs live in `apps/esa/internal/fleet/runs.go` and `runner.go`.

### Kinds

| Kind      | What each server does                                                     | Order                                                                  |
| --------- | ------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `measure` | `esa-helper clean --measure`: reports reclaimable space, changes nothing. | Parallel, 4 servers at a time, 10 minutes each                         |
| `clean`   | `esa-helper clean --apply` with the selected cleanups.                    | Parallel, 4 servers at a time, 10 minutes each                         |
| `update`  | Package upgrade, then an optional reboot.                                 | In batches of the batch size, stops after the failure limit is reached |
| `reboot`  | Reboot and wait for the server to come back.                              | In batches of the batch size, stops after the failure limit is reached |

`update` and `reboot` runs use two group settings, which a request can override for one run:

- Batch size (`batchSize`, 1 to 50, default 1): how many servers are worked on at the same time.
- Failure limit (`maxFailures`, 1 to 1000, default 1): how many failed servers stop the run. 1 stops at the first failure.

The runner takes the run's servers in name order and cuts them into batches of the batch size. The server that hosts the control plane always gets a batch of its own, so the batch before it can be short. The servers of a batch run at the same time, and the next batch starts only when every server in the batch is done. After each batch the runner counts the failed servers so far. When the count reaches the failure limit, it starts no more batches and marks the remaining servers `skipped` with "stopped after <name> failed" (one failure) or "stopped after <n> failed servers". The servers already running in a batch always finish, so a run can end with more failures than the limit. For a group of 200 servers at about 3 minutes each, a batch size of 10 cuts the run from about 10 hours to about 1 hour.

### Starting a run

`StartRun` needs the admin role and the `actions` scope. A run targets a group or a list of servers. All its servers must belong to one owning account, which owns the run; a run that mixes own and shared servers is refused. Options come from the group (cleanups, journal size, update mode, held packages, reboot if required, batch size, failure limit) or from the request. Options in a request replace the group's, except that a batch size or failure limit left out keeps the group's. Without a group the defaults are build cache and VS Code cleanup on, journal off, journal size `200M`, update mode `security`, batch size 1, and failure limit 1. The trigger is `schedule`, `manual`, `api`, or `mcp`.

The run executes in the background. Each server is locked while it is worked on, both in this process and with a PostgreSQL advisory lock held on one connection. A server another run or another control plane process already holds is marked `skipped`; in an update run that does not count as a failure.

### Updates

For each server, in parallel with the rest of its batch, the runner:

1. Calls `upgrade-precheck` (3 minute timeout). Problems fail the server.
2. Reads `status` and stores the failed units and unhealthy containers. A failed read fails the server before the upgrade starts. The group's checks run here too. A blocking failure stops that server before apt starts. A check dials the enrolled address, or asks the helper about one service or container. It does not follow an HTTP redirect.
3. Calls `upgrade-start --run-id <id> --mode security|all [--hold ...]`. The helper starts the upgrade as a transient systemd unit named `esa-upgrade-<run id>`, so it keeps running when the SSH session drops.
4. Polls `upgrade-status` every 10 seconds for up to 2 hours. Connection errors are retried, because the upgrade does not depend on SSH. A host key mismatch fails the server at once.
5. If the upgrade succeeded, the server needs a reboot, and the group has "reboot if required" on, it reboots and waits. A server marked as hosting the control plane is not rebooted here.
6. Reads `status` again. A failed unit that was not already failed, or a rise in unhealthy or restarting containers, fails that server. A status read that fails also fails that server. A problem that was already there does not. The same checks run again. A blocking failure fails that server. The failure limit can then stop the rest of the batch. Servers named as canaries run first, and one failed canary skips the rest even when the failure limit is higher. If the run asks for a watch, the canary checks repeat for that many seconds, up to 10 minutes, before the other servers start. A failure during the watch skips the rest. `GET /runs/{id}/report` lists each server's packages, checks, and skip reason.

### Reboots

`rebootAndWait` reads the boot time and the container list, calls `esa-helper reboot`, then checks `status` every 15 seconds until the boot time is more than 30 seconds later than before, for up to 10 minutes. After 20 more seconds it checks that every container that was running with a restart policy other than `no` is running again. A reboot run reboots one batch at a time, with the same batch size and failure limit as updates. A reboot run with the `schedule` trigger skips a server that hosts the control plane. Today the scheduler only starts `clean` and `update` runs.

### Finishing and resuming

When all servers are done, the run gets a summary such as "2 ok, 1 failed, 0 skipped" (plus bytes freed for `clean`) and a status: `succeeded`, `partial` (some servers failed and some succeeded), `failed` (some failed and none succeeded), or `skipped` (nothing failed and nothing succeeded). Any failure fires a `run_failed` alert.

At startup, and every minute after that, `ResumeRuns` looks for runs still marked `running`. A run whose worker still holds the lock is left alone. Upgrades keep running on the servers while the worker is gone, so for an `update` run it follows the upgrade of every server still marked `running` (the batch that was in flight, which can be several servers) until all of them finish. Failures from before the restart count toward the failure limit. Then it carries on with the queued servers in new batches, or skips them if the limit is reached. Any other kind is finished at once, with its unfinished servers marked failed with "interrupted: the worker that started this run is gone". At shutdown the control plane waits up to 20 seconds for background work.

## Alerts and notifications

Alert logic lives in `apps/esa/internal/fleet/alerts.go` and `alerts_manage.go`. Every account gets one rule per kind on first use:

| Kind              | Condition                                                                                                                                              | Default threshold | Default duration | Severity                                                 |
| ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------- | ---------------- | -------------------------------------------------------- |
| `disk`            | A filesystem is at or over the threshold                                                                                                               | 85%               | 0                | critical at 95%, else warning                            |
| `inode`           | Inode use on a filesystem                                                                                                                              | 85%               | 0                | critical at 95%, else warning                            |
| `memory`          | Memory use                                                                                                                                             | 90%               | 10 minutes       | critical at 97%, else warning                            |
| `unreachable`     | The server is `unreachable`                                                                                                                            |                   | 3 minutes        | critical                                                 |
| `failed_unit`     | A systemd unit failed                                                                                                                                  |                   | 0                | warning                                                  |
| `container`       | Containers are unhealthy or restarting                                                                                                                 |                   | 5 minutes        | warning                                                  |
| `reboot_required` | The server needs a reboot                                                                                                                              |                   | 7 days           | info                                                     |
| `cert_expiry`     | A certificate expires within the threshold, or its check failed                                                                                        | 14 days           | 0                | critical at 3 days, else warning                         |
| `run_failed`      | A run had a failed server                                                                                                                              |                   | 0                | warning                                                  |
| `host_key`        | The server presented a different host key                                                                                                              |                   | 0                | critical                                                 |
| `exposure`        | Password login, empty passwords, root password login, no firewall, an extra uid 0 account, or `NOPASSWD ALL`. Off until an admin enables it.           |                   | 0                | warning, or critical for empty passwords and extra uid 0 |
| `new_port`        | A socket bound to every interface that was not in the previous exposure report. Reachability is not verified. The first report only sets the baseline. |                   | 0                | warning, one notice                                      |
| `new_key`         | An SSH key fingerprint that was not in the previous access report. The first report only sets the baseline.                                            |                   | 0                | warning, one notice                                      |
| `security_update` | A security package has been pending for at least the threshold                                                                                         | 7 days            | 0                | warning, critical at 30 days                             |

After each poll, `evaluateAlerts` compares the server's current conditions with its open alerts. A new condition opens a pending alert. A pending alert fires once its condition has lasted the rule's duration; a duration of 0 fires at once. An alert whose condition is gone resolves. A host key mismatch or an unreachable server suppresses the other server alerts. Certificate alerts are evaluated by the certificate job and belong to the account rather than a server. A `run_failed` alert is a one-off: it is stored already resolved and only sends a notification.

Firing and resolving (of an alert that had fired) both send a message to every enabled channel of the account. `enqueueNotify` writes a delivery row first, then asks a worker to send it. A full worker queue leaves the row queued. `DrainDeliveries` sends queued rows, retries one left in sending, and marks a delivery dead after five failures. A maintenance window skips ordinary notices for that server, its group, or the whole space. Host key changes and new SSH keys are still sent. The "test" button sends right away instead.

`apps/esa/internal/notify` supports these channel kinds:

| Kind      | Delivery                                                                                                                                  |
| --------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `ntfy`    | POST to the topic URL with `Title`, `Priority` (urgent, high, default, or low when resolved), and `Click` headers. Optional access token. |
| `webhook` | POST of a JSON body. With a secret, the body is signed with HMAC-SHA256 in `X-Esa-Signature: sha256=<hex>`.                               |
| `discord` | POST to a Discord webhook URL.                                                                                                            |
| `slack`   | POST to a Slack webhook URL.                                                                                                              |
| `email`   | SMTP to one or more recipients.                                                                                                           |

All channel connections go through `netguard`, the same address policy as SSH. A failed delivery stores only an error category on the channel, because every member can see it; the detail goes to the log. Only owners can create channels, since the configuration holds credentials.
