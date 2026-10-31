# easy-sys-admin: plan and status

`../PLAN.md` is the original product draft, and its cleanup rules and "rules with no switch" still hold. This file records what was decided while building, what exists now, and what is left. `../README.md` covers how to deploy and configure it.

## Decisions

| Topic              | Decided                                                                                                                                                                                                    |
| ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Repo               | pnpm + Turborepo monorepo. Node 26, Go 1.27, one pnpm catalog for shared versions                                                                                                                          |
| Backend            | Go: control plane `esa serve`, CLI `esa`, server binary `esa-helper`                                                                                                                                       |
| Frontend           | TanStack Start (SSR) with Tailwind CSS 4. It serves the landing page, the board, and MCP                                                                                                                   |
| Database           | PostgreSQL. GORM with AutoMigrate under an advisory lock; raw SQL only where GORM gets awkward                                                                                                             |
| API contract       | `packages/api/openapi.yaml`. Go handlers, the Go CLI client, and TypeScript types are generated from it                                                                                                    |
| Connecting servers | SSH plus `esa-helper`. The `esa` user may run only the helper through sudo. Enroll with a pasted one-time command, or with a one-time sudo login that is then forgotten                                    |
| Upgrades           | Run as a transient systemd unit, so they survive dropped SSH and control plane restarts. One server at a time; the first failure stops the group                                                           |
| MCP                | Streamable HTTP at `/mcp` (TypeScript SDK v2). Tools follow the API token's scopes                                                                                                                         |
| Sign-in            | Email and password with sign-up, Google, any OIDC provider (Authentik), and a loopback-only dev login. Any mix, set by env                                                                                 |
| Deploy             | One compose stack (PostgreSQL, control plane, web) behind Traefik, with no published ports. GitHub Actions builds and deploys on a self-hosted runner; settings come from production environment secrets and repository variables |

## Built and verified

Verified against a real Ubuntu 24.04 arm64 server (`Server_Paid`) unless noted.

- Enrollment: the one-time sudo login path, and the pasted command path (a reused command is refused). The host key probe sends no credentials. A changed host key pauses all work until a fingerprint that matches is accepted.
- Live status every minute: CPU, memory, disks and inodes, load, network, time sync, reboot required, failed units, Docker. Hourly rollups, 48 hours raw, 90 days hourly.
- Updates: pending list with security detection, security-only or full upgrades with held packages, survives `kill -9` of the control plane mid-upgrade.
- Reboots: reboot, wait for a new boot time, check containers with a restart policy came back.
- Cleanups: build cache (skipped during builds), old VS Code server copies, journal cap.
- Services, containers (including pull and recreate of one compose service), logs, access, security, scheduled jobs, image update checks. Protected units cannot be stopped; protected containers need their name typed.
- Groups with schedules. A due slot runs once; a slot missed by over an hour is recorded as missed (tests).
- Alerts fire, wait out their duration, and resolve (tests). ntfy delivery verified; webhooks to loopback and link-local addresses are refused. TLS certificate checks.
- API tokens with scopes, account isolation, audit log marking web, API, and MCP calls.
- Web app flows in a real browser: sign-up, sign-in, groups, runs, server settings, logs, add server.
- CLI: login, status, servers, groups, runs (with `--wait`), alerts.
- MCP: tool list follows scopes, `fleet_summary`, `list_updates`, `start_run`.

## Left to do

- Invite other people into an account (roles exist; there is no invite flow yet).
- Password reset by email.
- Agent mode for servers without inbound SSH.
- RHEL-family servers (dnf).
- Live log follow over SSE.
- Configurable poll interval and retention (now fixed at 1 minute, 48 hours, 90 days).
