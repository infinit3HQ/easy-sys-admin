# Security model

This document describes how easy-sys-admin protects the control plane, the people who use it, and the servers it manages. It covers secrets at rest, sign-in, sessions, API tokens, the MCP endpoint, CSRF protection, the outbound address policy, request size limits, rate limits and caps, security headers, log redaction, the boundary of the `esa` user on servers, team boundaries, and the audit log. It ends with how to report a vulnerability. Component names and request paths are explained in [architecture.md](architecture.md).

## Secrets at rest

### What is sealed

`crypto.Box` in `apps/esa/internal/crypto/secretbox.go` seals secrets with AES-256-GCM under a key from `ESA_MASTER_KEY`. Each secret is bound to the row it belongs to: the context string is used as GCM additional data, so a ciphertext copied into another row does not decrypt.

| Secret                                                                             | Table                | Context             |
| ---------------------------------------------------------------------------------- | -------------------- | ------------------- |
| The account's ed25519 SSH private key                                              | `account_keys`       | `account_key:<id>`  |
| A sudo login kept with `keepLogin`                                                 | `server_credentials` | `server_login:<id>` |
| Notification channel configuration (URLs, tokens, webhook secrets, SMTP passwords) | `notify_channels`    | `channel:<id>`      |

A sealed value looks like `v1:<key id>:<base64 of nonce and ciphertext>`. The key id is the first 8 hex digits of the SHA-256 of the key.

`ESA_MASTER_KEY` must be 32 random bytes, base64-encoded (`openssl rand -base64 32`). The control plane does not start without it. It lives only in the control plane's environment, never in the database.

### Rotating the master key

`ESA_MASTER_KEY` takes a comma-separated list. The first key seals new secrets. Every key in the list can open existing ones.

1. Generate a new key.
2. Set `ESA_MASTER_KEY=<new>,<old>` and stop the control plane.
3. Run `esa reseal`. `--dry-run` counts rows and writes nothing.
4. When the command says live rows are sealed with the current key, remove `<old>` from `ESA_MASTER_KEY` and start the control plane again.

A row that cannot be opened is left unchanged, and the command exits with an error so the old key stays until you fix that row. Running it again is safe. A database backup taken before the re-seal still needs the old key. This does not rotate the SSH key on a server, and it does not prove an old copy of the key was destroyed. It is not tied to one user, one organization, or a maintenance plan. There is no API or MCP call for it, because one re-seal rewrites sealed secrets for every account on this control plane.

### Hashed tokens

These are stored only as a SHA-256 hash, so a copy of the database does not reveal them:

| Token          | Format and lifetime                             |
| -------------- | ----------------------------------------------- |
| Session cookie | 32 random bytes; 24 hours idle, 7 days at most  |
| API token      | `esa_` and 32 random bytes; 1 to 365 days       |
| Enroll command | `enr_` and 32 random bytes; 15 minutes, one use |
| Team invite    | 7 days, one use (see [teams.md](teams.md))      |
| Share link     | 7 days, one use (see [teams.md](teams.md))      |

## Sign-in

Sign-in methods are set with environment variables (`apps/esa/internal/config/config.go`). Any combination can be on, and at least one must be.

| Method             | Settings                                                                                    | Notes                                                                                                                                                                                                                                                   |
| ------------------ | ------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Email and password | `ESA_AUTH_PASSWORD` (default `true`), `ESA_SIGNUP` (default `false`), optional `ESA_SMTP_*` | Passwords are 10 to 200 characters, hashed with argon2id (19 MiB, 2 iterations, 1 lane, 16-byte salt). With SMTP configured, a password account cannot sign in until it opens the confirmation link. Google sign-in does not send or require that link. |
| OpenID Connect     | `ESA_OIDC_ISSUER`, `ESA_OIDC_CLIENT_ID`, `ESA_OIDC_CLIENT_SECRET`, `ESA_OIDC_PROVIDER_NAME` | Authorization code flow with PKCE (S256), with state and nonce checked. An email is stored only when the provider says `email_verified` is true.                                                                                                        |
| Google             | `ESA_GOOGLE_CLIENT_ID`, `ESA_GOOGLE_CLIENT_SECRET`                                          | The same OIDC flow against `https://accounts.google.com`. The user is stored with the email marked confirmed.                                                                                                                                           |
| Dev mode           | `ESA_AUTH_MODE=dev`                                                                         | Signs everyone in as one local user. Refused unless `ESA_LISTEN` and `ESA_PUBLIC_URL` are loopback addresses, and `/auth/dev-login` only answers loopback requests.                                                                                     |

More rules:

- `ESA_ALLOWED_EMAILS` limits who may sign in with any method. It is checked on every session and token use, so removing someone from the list ends their sessions and disables their tokens.
- When `ESA_ALLOWED_EMAILS` is set, `ESA_SIGNUP=true` is ignored with a warning at startup, because a password sign-up cannot prove the person owns the address.
- The first user of an empty database can always create a password account while password sign-in is on.
- The same email is one user. Google or OIDC sign-in with a verified address joins the account that already uses that email and marks it confirmed. A password sign-up for an address that already uses Google adds a password to that account only after the person opens the link. With system mail off, that sign-up is refused and the person uses the provider instead.
- Argon2 work runs in at most as many slots as the machine has CPUs. A sign-in that waits more than 5 seconds for a slot gets a "busy" answer instead of piling up memory.
- After sign-in, the `return_to` target must be a same-site path. Anything else redirects to `/app`.

## Sessions

Sessions are in `apps/esa/internal/auth/sessions.go`.

- The cookie is `__Host-esa_session` when `ESA_PUBLIC_URL` is https, otherwise `esa_session`. It is `HttpOnly`, `SameSite=Lax`, `Path=/`, and `Secure` on https.
- A session ends after 24 hours without use, or 7 days after sign-in, whichever comes first. `last_seen_at` is updated at most every 5 minutes.
- Each sign-in creates a new token. Any session the request already carried is deleted first, so a token planted before sign-in is never upgraded.
- Sign out ends the current session. "Sign out everywhere" (`/auth/logout-all`) deletes every session of the user.
- If the user is removed from the session's account, the session moves to another account they belong to, or ends if there is none.
- The hourly sessions job deletes expired and idle sessions.

## API tokens

API tokens are for the CLI and MCP clients (`apps/esa/internal/fleet/tokens.go`). A request with `Authorization: Bearer ...` never falls back to cookies; the control plane drops the `Cookie` header before the session middleware runs.

| Scope       | Allows                                                      | Role needed to grant                  |
| ----------- | ----------------------------------------------------------- | ------------------------------------- |
| `read`      | Everything a viewer sees, except logs                       | viewer                                |
| `logs:read` | Server and container logs                                   | admin, or a viewer in at least 1 team |
| `actions`   | Runs, service and container actions, reboots, update checks | admin, or a viewer in at least 1 team |
| `admin`     | Servers, groups, alert rules, channels, certificates        | admin                                 |

- Every token has `read`; it is added if missing.
- A token expires after 1 to 365 days. The default is 90 days. Every new token expires.
- The secret is shown once. The database keeps its SHA-256 and a 10 character prefix for display.
- Scopes apply on top of the user's role, and the role is read at each request. Demoting or removing a member limits or disables their tokens at once. Browser sessions are limited by role only.
- A viewer in a team can grant `logs:read` and `actions` because the team makes them admin of some groups. The token still works only where their role on the group allows it, checked at each request. Removing them from the team ends that at once.
- A token cannot list, create, or revoke tokens, and cannot create or accept invites or share links.
- Revoked and expired tokens get 401.

## MCP

The MCP server at `/mcp` runs in the web app (`apps/web/src/server/mcp.ts`). It has no access of its own. Every tool calls the control plane API with the caller's API token, so an agent can do what that token and its user's role allow and nothing more. Calls are recorded in the audit log with source `mcp`.

### Plans

A group can require an approved plan (`apps/esa/internal/fleet/plans.go`). Then every clean and update on it must come from a plan, including scheduled runs, so the group's cleanup and update schedules must be off. A token with `actions` and without `logs:read` can propose and execute a plan. Only a person signed in to the web app, with the admin role, can approve or reject one. An approval lasts 30 minutes and covers the servers and pending packages recorded when the plan was proposed; a change to either cancels it. A plan run keeps the group's held packages, and the helper installs only the approved packages: if apt would also upgrade anything else, it installs nothing and the server fails. A plan update is refused on a helper too old to do that.

A token, even with the `admin` scope, cannot weaken the rule. Turning off "Require an approved plan", deleting such a group, and moving a server out of it (to no group or to a group without the rule) need a person in the web app. The `group.update` audit entry records the rule before and after, the held packages, and the check names.

Before handling a request, `/mcp`:

1. Refuses with 403 any request whose `Origin` header is set and differs from the origin of `ESA_PUBLIC_URL` (or the request's own origin when that is unset). Browsers send `Origin`, so a page on another site cannot drive the endpoint with a token the browser holds.
2. Checks the bearer token with `GET /api/v1/me` and answers 401 if it is invalid, expired, or revoked.

The tools a client sees depend on the token's scopes:

| Tools                                                                                                                                                                                                                                                                          | Shown when                                            |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------- |
| `fleet_summary`, `get_server_status`, `list_groups`, `list_runs`, `get_run`, `list_alerts`, `list_updates`, `list_services`, `list_containers`, `check_container_images`, `get_security_report`, `get_access_report`, `get_scheduled_jobs`, `get_metrics`, `list_certificates` | Always (every token has `read`)                       |
| `get_logs`                                                                                                                                                                                                                                                                     | The token has `logs:read`                             |
| `start_run` (`measure`, `clean`, or `update`)                                                                                                                                                                                                                                  | The token has `actions` and does not have `logs:read` |

`start_run` is hidden when a token also has `logs:read` because log lines are written by whatever runs on the servers. Text in a log can try to instruct the agent, for example to start an update run. Keeping log reading and run starting on separate tokens means text an agent reads through MCP cannot lead it to change the fleet with the same token. The server instructions tell the agent this and tell it to treat server output as untrusted data. This rule is specific to MCP; the API and CLI accept a token with both scopes. `start_run` also refuses groups and servers shared with view access only, and it cannot start reboots.

## CSRF and cross-origin requests

- The control plane wraps every route in Go's `http.CrossOriginProtection` (`crossOriginProtection` in `apps/esa/internal/server/middleware.go`). It rejects state-changing requests that the browser marks as cross-site through `Sec-Fetch-Site`, or whose `Origin` does not match the host. `ESA_PUBLIC_URL` is a trusted origin. GET, HEAD, and OPTIONS pass.
- Requests without those browser headers, such as the CLI, pass. They authenticate with bearer tokens, which a browser never attaches on its own.
- The session cookie is `SameSite=Lax`, so other sites' form posts do not carry it.
- The web app keeps TanStack Start's CSRF middleware for its server functions (`apps/web/src/start.ts`).
- `/mcp` has its own `Origin` check, described above.

## Outbound connections

`apps/esa/internal/netguard` decides which addresses esa may connect to on a user's behalf. It covers SSH to servers (including host key probes and the enroll host key check), certificate checks, and every notification channel, HTTP and SMTP alike. The check runs inside the dialer after DNS resolution, for every address actually dialled, so a name that resolves or later rebinds to a blocked address is refused.

| Addresses                                                                                                                                                                              | Policy                                              |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------- |
| Unspecified, multicast, link-local (including `169.254.169.254`), `0.0.0.0/8`, `fd00:ec2::254` (AWS metadata over IPv6), `100.100.100.200` (Alibaba Cloud metadata), `255.255.255.255` | Always refused                                      |
| Loopback, private ranges (`10/8`, `172.16/12`, `192.168/16`, IPv6 ULA), CGNAT `100.64.0.0/10`, and anything else that is not global unicast                                            | Refused unless `ESA_ALLOW_PRIVATE_NETWORKS` is true |
| Global unicast                                                                                                                                                                         | Allowed                                             |

`ESA_ALLOW_PRIVATE_NETWORKS` defaults to true when sign-up is closed and false when anyone can sign up, because an open install should not let strangers probe the host's network. A NAT64 address (`64:ff9b::/96`) is judged by the IPv4 address inside it.

## Request limits

| Limit                                                                                                                | Where                                                            |
| -------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| Bodies over 1 MiB get 413, judged by `Content-Length`                                                                | Web proxy, `apps/web/src/server/proxy.ts`                        |
| Bodies for `/api/`, `/auth/`, and `/enroll/` are read up to 1 MiB, chunked or not, and get 413 beyond that           | `limitBodies` in `apps/esa/internal/server/middleware.go`        |
| A scan report imported at `POST /api/v1/servers/{id}/scan` may be up to 10 MiB; every other body keeps the 1 MiB cap | `bodyLimit` in `middleware.go`, `SCAN_IMPORT_PATH` in `proxy.ts` |
| The enroll host key report is read up to 16 KiB                                                                      | `apps/esa/internal/api/enroll.go`                                |
| Header read 10 s, body read 60 s, response write 10 min, idle 120 s                                                  | Control plane HTTP server                                        |
| 60 s per proxied call, 11 min for server, certificate, and enroll calls                                              | Web proxy                                                        |
| API tokens over 200 characters and enroll tokens over 100 are rejected without a lookup                              | `fleet`                                                          |

The proxy also strips headers a client must not set (`x-real-ip`, `forwarded`, `x-esa-client`, `x-forwarded-host`, `x-forwarded-proto`). Sign-in rate limits use the rightmost `X-Forwarded-For` entry, which Traefik appends. If the control plane is ever reachable without Traefik in front, clients can choose that value, and only the per-email and global limits still hold.

## Rate limits and caps

### Rate limits

| What                                                                                                                                                                              | Limit                                      | Source                                                   |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------ | -------------------------------------------------------- |
| Password sign-in, per email and client address                                                                                                                                    | 5 failures per 15 minutes                  | shared rows in PostgreSQL (`sign_in_hits`)           |
| Password sign-in, per email                                                                                                                                                       | 100 failures per 15 minutes                | same                                                 |
| Password sign-in, per client address                                                                                                                                              | 30 failures per 15 minutes                 | same                                                 |
| Password sign-in, whole install                                                                                                                                                   | 1000 failures per 15 minutes               | same                                                 |
| Password sign-up, per client address                                                                                                                                              | 10 attempts per hour, successful or not    | `apps/esa/internal/auth/handlers.go`                     |
| Probing a host key, adding a server, reinstalling a helper, adding a certificate, testing a channel, creating an organization, creating an extra team, an invite, or a share link | 20 per minute per account, for each action | `limitRate` in `apps/esa/internal/fleet/limits_state.go` |

A successful sign-in clears the per-pair and per-email counters only. The per-client and global budgets stay, so signing in to your own account does not buy more guesses at others. Scheduled work done by the system is not rate limited.

Two more limits protect the servers and the control plane:

- Servers that keep refusing esa's SSH login are polled less often: no delay for the first two failures, then 2, 4, 8, and 16 minutes, capped at 30. Repeated failed logins could get esa's address banned by fail2ban.
- Alert notifications go through a queue of 1000 messages served by 4 workers, 20 seconds per delivery. When the queue is full, messages are dropped and logged.

### Caps

| Item                              | Cap                                                                                   | Source                  |
| --------------------------------- | ------------------------------------------------------------------------------------- | ----------------------- |
| Servers per account               | 500 by default, set by `ESA_MAX_SERVERS_PER_ACCOUNT`. 10 when `ESA_BETA_LIMITS` is on | `fleet/limits_state.go` |
| Certificates per account          | 200                                                                                   | same                    |
| Notification channels per account | 25                                                                                    | same                    |
| Organizations a user may own      | 20, or 1 when `ESA_BETA_LIMITS` is on. The personal space is not counted              | `fleet/teams.go`        |
| Members per organization          | 100                                                                                   | same                    |
| Pending invites per organization  | 50                                                                                    | same                    |
| Extra teams per organization      | 100                                                                                   | `fleet/orgteams.go`     |
| Groups per extra team             | 200                                                                                   | same                    |
| Shares per group                  | 50                                                                                    | `fleet/sharing.go`      |
| Pending share links per group     | 50                                                                                    | same                    |

## Security headers

The web app sets headers on its pages in `apps/web/src/start.ts`. Paths under `/api`, `/auth`, and `/enroll` keep the control plane's headers instead.

| Header                      | Web pages                                              | Control plane responses                      |
| --------------------------- | ------------------------------------------------------ | -------------------------------------------- |
| `Content-Security-Policy`   | See below. Not set by the Vite dev server.             | `default-src 'none'; frame-ancestors 'none'` |
| `X-Frame-Options`           | `DENY`                                                 | `DENY`                                       |
| `X-Content-Type-Options`    | `nosniff`                                              | `nosniff`                                    |
| `Referrer-Policy`           | `same-origin`                                          | `same-origin`                                |
| `Strict-Transport-Security` | `max-age=31536000` when the request arrived over https | Not set                                      |

The page policy uses a fresh nonce for each response:

```
default-src 'self'; script-src 'self' 'nonce-<random>'; style-src 'self' 'unsafe-inline';
img-src 'self' data:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none';
base-uri 'none'; object-src 'none'; form-action 'self'
```

`style-src` allows inline styles because React renders style attributes on the server. `font-src` allows `data:` because Vite inlines small font subsets.

## Metrics endpoint

`GET /metrics` on the control plane needs no login. It is served only on the control plane's own address (the internal Docker network, or loopback by default), the web server does not proxy it, and it reports counts with fixed labels, never names, hosts, emails, or tokens. `ESA_METRICS=false` turns it off.

## Logs

- The request log records method, path, status, and duration. It never logs query strings, because OIDC callbacks carry the authorization code there. Successful `/healthz` checks and `/metrics` scrapes are not logged.
- `api.RedactPath` replaces the token segment of paths under `/enroll/`, `/api/v1/invites/`, `/api/v1/shares/`, `/invite/`, and `/share/` with `[redacted]`. It applies to request logs, panic logs, and API error logs.
- Notification errors name only the host, because a webhook URL can hold a secret. The channel's visible `last_error` holds only an error category; the detail goes to the server log.
- Errors from a helper install have the sudo password replaced with `[password]`.

## The esa user on servers

On each managed server esa logs in as a system user `esa` whose key can only run `esa-helper ssh-dispatch`. That command only passes `sudo -n /usr/local/lib/esa/esa-helper <action>` on to sudo, and the sudo rule allows only the helper. The helper runs fixed programs with typed arguments and refuses actions that could cut the server off, such as stopping sshd or touching the firewall. It has no self-update action, so the `esa` key cannot install a different binary as root. Every SSH connection pins the server's host key and stops before sending a credential if the key changed. The details are in [server-helper.md](server-helper.md).

## Teams and sharing

Every account-owned query starts from the caller's account, and one visibility helper adds what is shared with it. Organization roles are `owner`, `admin`, and `viewer`, and new members start as viewers.

Anything that acts on a server group or its servers uses the caller's role on that group (`roleOn` in `apps/esa/internal/fleet/roles.go`). It is the organization role, except that a viewer who is in a team holding the group is admin of it. A team can hold its organization's groups and groups another organization shares in. On a shared group the role is then limited by the share: `view` stays viewer, `operate` gives admin at most. Admin on a group allows runs, service and container actions, update checks, logs, cron command lines, and, for the organization's own groups, editing the group's settings. Everything else stays on the organization role: adding, changing, moving, and deleting servers, host keys, helper reinstall, enrollment, creating and deleting groups, sharing, alert rules, channels, certificates, members, and teams. Token scopes apply on top in every case. Server and group responses carry `yourRole` so the web app can hide controls, but the API makes the decision.

A shared group gives another organization `view` or `operate` access to the group and the servers in it at that moment. The owning organization keeps the SSH key, host keys, enrollment, settings, and deletion. A run's servers must all belong to one owning account. API tokens cannot create or accept invites or share links. The full rules, including how invites and share links stop working when their creator loses authority, are in [teams.md](teams.md). Tests in `apps/esa/internal/fleet/isolation_test.go`, `sharing_test.go`, and `roles_test.go` check these boundaries.

## Audit log

The `audit_log` table records who did what. Each entry has the account, the user, an action, a target, a JSON detail, the source (`web`, `api`, `mcp`, or `system`), and a time. Calls made with an API token add `token_id` to the detail.

Recorded actions include:

| Area                 | Actions                                                                                                                                                                                                                                                                                                                       |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Sign-in and accounts | `auth.sign_in`, `auth.sign_up`, `account.create`, `session.switch_account`, `org.create`, `org.rename`, `org.rename_team`, `org.delete`                                                                                                                                                                                       |
| Servers              | `server.probe`, `server.add`, `server.enroll_start`, `server.enroll_token`, `server.enroll_complete`, `server.update`, `server.delete`, `server.reinstall_helper`, `server.accept_host_key`, `server.host_key_mismatch`, `server.read_logs`, `server.exposure`                                                                |
| Operations           | `run.start`, `service.<action>`, `container.<action>`, `container.update`, `plan.propose`, `plan.approve`, `plan.reject`, `plan.execute`, `plan.invalidate`, `plan.cancel`                                                                                                                                                    |
| Settings             | `group.*`, `alert_rule.update`, `channel.*`, `cert.add`, `cert.delete`, `token.create`, `token.revoke`, `digest.sent`, `incident.acknowledge`, `incident.snooze`, `incident.accept`, `routing.update`, `delivery.retry`, `identity.save`, `run.gate`, `key.label`, `key.baseline`, `access.baseline`, `key.exception`, `key.exception.delete`, `maintenance.create`, `maintenance.delete`, `dataset.create`, `dataset.delete`, `recovery.submit`, `vuln.exception`, `vuln.exception.delete` |
| Teams and sharing    | `team.create`, `team.update`, `team.delete`, `team.add_member`, `team.remove_member`, `member.role`, `member.remove`, `member.leave`, `invite.create`, `invite.revoke`, `invite.accept`, `share.link_create`, `share.link_revoke`, `share.accept`, `share.update`, `share.remove`                                             |

When someone acts on a server through a share, the entry goes to the owning account with `via_account_id`, and a copy goes to the caller's account with `owner_account_id`. Reading logs is recorded too, because logs can hold secrets.

Owners and admins read the log on the Activity page or through `GET /api/v1/activity` (100 entries by default, at most 500). Entries are kept until their account is deleted. If a user is deleted, their entries stay with the user set to empty. There is no pruning job.

## Reporting a vulnerability

Please do not open a public issue for a security problem. Open a private security advisory on the GitHub repository instead (the Security tab, then "Report a vulnerability"). Include what you found, how to reproduce it, and which version or commit you tested.
