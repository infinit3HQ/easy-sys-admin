# easy-sys-admin

One board for a fleet of Linux servers. Add servers, put them in groups, and set when each group gets its disks cleaned and its packages patched. The board shows live status, and you can share a group with another organization. AI agents read the same data, and can start runs, over MCP.

Nothing listens on your servers. The control plane connects over SSH as a restricted `esa` user that can only run one small helper binary.

## What it does

- Live status every minute: CPU, memory, disks and inodes, load, network, time sync, failed units, Docker, reboot required. History for 90 days.
- Package updates, security-only or all, in batches of servers you choose (one at a time by default), stopping after a set number of failures. Upgrades run in a systemd unit, so a dropped connection does not break them. Optional reboot, then a check that containers came back.
- Cleanups that stay small and strict: idle Docker build cache, old VS Code server copies, a journal size cap. Never volumes, never images in use.
- Services, containers, logs, scheduled jobs, access and security reports for each server.
- Alerts for disk, inodes, memory, unreachable servers, failed units, unhealthy containers, certificates and failed runs, sent to ntfy, webhooks, Discord, Slack or email.
- A private personal space for every user, organizations with owner, admin, and viewer roles, extra teams that grant admin on chosen groups, and groups shared between organizations with view or operate access.
- A web app, a CLI and an MCP endpoint on top of one API.

## How it fits together

```
browser / CLI / AI agent ──HTTPS──> web (TanStack Start) ──> control plane (Go) ──> PostgreSQL
                                                                    │
                                                                    └─SSH as "esa"─> esa-helper on each server
```

| Path                     | What it is                                                                                  |
| ------------------------ | ------------------------------------------------------------------------------------------- |
| `apps/esa`               | Go: the control plane (`esa serve`), the `esa` CLI, and `esa-helper` for managed servers    |
| `apps/web`               | TanStack Start: landing page, the board, docs at `/docs`, the MCP endpoint, and a proxy to the API |
| `packages/api`           | `openapi.yaml`, the API contract. Go and TypeScript code is generated from it               |
| `packages/plans`         | Plan names, prices, and the hosted beta caps                                                |
| `packages/eslint-config` | Shared ESLint config                                                                        |
| `packages/tsconfig`      | Shared TypeScript configs                                                                   |
| `deploy`                 | `compose.yaml` and `.env.example` for one Docker host behind Traefik                        |
| `docs`                   | Documentation, listed below                                                                 |

## Quick start

You need PostgreSQL, Go 1.27, Node 26 and pnpm 12.

```sh
createdb esa_dev
cp .env.example .env               # then set ESA_MASTER_KEY=$(openssl rand -base64 32)
pnpm install
pnpm --filter @esa/esa dev         # control plane on 127.0.0.1:8080
pnpm --filter @esa/web dev         # web on http://localhost:3000
```

Open http://localhost:3000 and use "Continue as the local dev user". Details are in [docs/development.md](docs/development.md).

## Deploy

```sh
cp deploy/.env.example deploy/.env  # set ESA_HOST, ESA_PUBLIC_URL, ESA_MASTER_KEY, POSTGRES_PASSWORD
docker compose -f deploy/compose.yaml --env-file deploy/.env up -d --build
```

Traefik must already run on the host. No ports are published; one hostname serves the app, the API, MCP and the enroll scripts. Deploying from GitHub Actions on a self-hosted runner is covered in [docs/deployment.md](docs/deployment.md). Every setting is an environment variable, listed in [docs/configuration.md](docs/configuration.md).

## Add a server

On the board, choose "Add server". Either paste the one-time command it gives you on the server:

```sh
curl -fsSL https://esa.example.com/enroll/enr_... | sudo sh
```

or give esa a sudo login once, which it uses to install the helper and then forgets. Both pin the server's host key. [docs/server-helper.md](docs/server-helper.md) lists exactly what gets installed.

## CLI and MCP

```sh
esa login https://esa.example.com --token esa_...   # token from Settings > API tokens
esa status
esa runs start clean --group production --wait
```

Point an MCP client at `https://esa.example.com/mcp` with an API token as the bearer token. The tools follow the token's scopes. See [docs/cli.md](docs/cli.md) and [docs/mcp.md](docs/mcp.md).

## Documentation

User documentation is served by the web app at `/docs`. The pages below are the contributor notes in this repository.

| Page                                         | Covers                                                           |
| -------------------------------------------- | ---------------------------------------------------------------- |
| [architecture.md](docs/architecture.md)      | Components, request flow, data model, background jobs, runs      |
| [configuration.md](docs/configuration.md)    | Every environment variable                                       |
| [deployment.md](docs/deployment.md)          | Compose, GitHub Actions, upgrades, backups, rollbacks            |
| [development.md](docs/development.md)        | Local setup, scripts, tests, adding endpoints and helper actions |
| [server-helper.md](docs/server-helper.md)    | What runs on a managed server and its safety rules               |
| [teams.md](docs/teams.md)                    | Personal spaces, organizations, teams, invites, shared groups    |
| [security.md](docs/security.md)              | Security model and how to report a vulnerability                 |
| [cli.md](docs/cli.md), [mcp.md](docs/mcp.md) | The command line and the MCP endpoint                            |
| [decisions.md](docs/decisions.md)            | Why the stack and design are what they are                       |
| [status.md](docs/status.md)                  | What is built and what is left                                   |

## License

Copyright (C) 2026 Niraj Dilshan and contributors.

You can self-host Easy Sys Admin for any use, including commercial use. There is no license fee and the license sets no usage cap. The install is in the repository, under `deploy/`, and on the web app at `/docs/self-hosting`. The default is 500 servers per account. That number is a setting, not part of the license.

The license is the GNU Affero General Public License, version 3 only (AGPL-3.0-only). It is not "or any later version".

Easy Sys Admin will remain available under AGPL-3.0-only. We do not use a CLA and cannot relicense contributors' code.

If you modify the software and let other people use that modified version over a network, the AGPL says you must offer them the source of the version they are talking to. The web footer has a Source code link for that. Point `ESA_SOURCE_URL` at the source of the build you are running when it is not the upstream repository. Managed Cloud, when it exists, is our hosted service. It is not a separate license.

The Easy Sys Admin name and logo belong to Niraj Dilshan (infinit3HQ). They are not covered by the license. See [TRADEMARKS.md](TRADEMARKS.md).

The software is provided as is, without warranty of any kind, to the extent the license allows. The authors are not liable for damage from using it. The full text is [LICENSE](LICENSE).

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md). Contributions use the [Developer Certificate of Origin](DCO) (`git commit -s`), not a contributor license agreement. Coding agents start at [AGENTS.md](AGENTS.md). Report security problems privately, as described in [SECURITY.md](SECURITY.md).
