---
title: Self-hosting
description: Requirements, Docker Compose, a binary install, Postgres, TLS, auth, upgrades, backups, and current limits.
---

You can run this source for any use, including commercial use. There is no license fee and the license sets no usage cap. There is no separate packaged download. Build it from the repository, or use the Docker Compose file in `deploy/`.

With the beta caps left off, one account can hold 500 servers and one person can own 20 organizations besides their personal space. Raise or lower the server cap with `ESA_MAX_SERVERS_PER_ACCOUNT` (1 to 100000). That is a setting, not a license limit.

## Requirements

- A Linux host with Docker Engine and the Compose plugin, or a host where you can run the `esa` binary and Node for the web app.
- PostgreSQL 18. Compose starts one for you. A binary install uses a database you already have.
- A DNS name and TLS in front of the web app. The compose file expects Traefik on an external Docker network named `proxy`, with an entrypoint `websecure` and a certificate resolver. Those names are settings.
- Outbound SSH from the control plane to each server you add, on that server's SSH port.
- 32 random bytes for `ESA_MASTER_KEY`, from `openssl rand -base64 32`. Store a copy somewhere other than the host. Without it, stored server logins and notification credentials cannot be read.

Managed servers must be Linux on amd64 or arm64, with `sudo`. Package updates use `apt` only.

## Docker Compose

Traefik must already be running and attached to the `proxy` network. No ports are published. One hostname serves the board, `/api/v1`, `/auth`, `/mcp`, and `/enroll`.

```sh
docker network create proxy   # skip if it already exists
cp deploy/.env.example deploy/.env
chmod 600 deploy/.env
```

Set at least:

- `ESA_HOST`, for example `esa.example.com`
- `ESA_PUBLIC_URL=https://esa.example.com` with no path
- `ESA_MASTER_KEY`
- `POSTGRES_PASSWORD`, from `openssl rand -hex 24`, so the value is safe inside a database URL

Leave `ESA_BETA_LIMITS` unset. Leave `ESA_SEO` unset unless you want the public pages in search engines. Leave `ESA_SIGNUP` false unless you want anyone who can reach the site to create an account. The first account can always be created while the database has no users.

```sh
docker compose -f deploy/compose.yaml --env-file deploy/.env up -d --build
docker compose -f deploy/compose.yaml --env-file deploy/.env ps
curl -fsS https://esa.example.com/healthz
```

A healthy body is `{"web":"ok","controlPlane":"ok"}`.

The stack is three containers:

| Container  | Role                                                                                                                         |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `postgres` | PostgreSQL 18. The data volume is `<stack>_postgres`, `esa_postgres` by default. The database network has no outside access. |
| `esa`      | Control plane on port 8080, inside the stack network. It is the only container that talks to PostgreSQL.                     |
| `web`      | Web app on port 3000. Traefik sends the hostname to it. It proxies `/api`, `/auth`, and `/enroll` to `http://esa:8080`.      |

If your Traefik names differ, set `TRAEFIK_NETWORK`, `TRAEFIK_ENTRYPOINT`, and `TRAEFIK_CERT_RESOLVER`.

Register OIDC at `$ESA_PUBLIC_URL/auth/callback/oidc`, or Google at `$ESA_PUBLIC_URL/auth/callback/google`, when you turn those on. Password sign-in is on by default. At least one sign-in method must be on or the control plane will not start.

Password confirmation and reset need `ESA_SMTP_HOST` and the rest of the SMTP settings. Leave the host empty to send no auth mail. A new password account cannot sign in until it opens the confirmation link when mail is on.

## Binary install

Build the control plane and the helpers, then the web app:

```sh
pnpm install
pnpm --filter @esa/esa build
pnpm --filter @esa/web build
```

Create a database and set the variables the control plane requires. The full list is in the repository file `docs/configuration.md`. The ones you cannot skip:

```sh
export ESA_DATABASE_URL=postgres://esa:secret@localhost:5432/esa
export ESA_MASTER_KEY=$(openssl rand -base64 32)
export ESA_PUBLIC_URL=https://esa.example.com
export ESA_LISTEN=127.0.0.1:8080
export ESA_HELPER_DIR=./apps/esa/bin
apps/esa/bin/esa serve
```

In another terminal, the built web server:

```sh
export ESA_API_URL=http://127.0.0.1:8080
export HOST=127.0.0.1
pnpm --filter @esa/web start
```

Put TLS in front of the web server. Enroll commands need `https` on `ESA_PUBLIC_URL`, except on localhost. Point the proxy at port 3000 and forward the original host. Do not publish the control plane's port 8080. `GET /metrics` on that port has no login. The web app does not proxy it.

`ESA_HELPER_DIR` must contain `esa-helper-linux-amd64` and `esa-helper-linux-arm64`. `pnpm --filter @esa/esa build` writes them to `apps/esa/bin`.

## Source offer

The footer link "Source code" is how people get the source of the running build, which the AGPL requires when you let others use a modified version over a network.

| Variable         | Default                                        | Meaning                                                                                    |
| ---------------- | ---------------------------------------------- | ------------------------------------------------------------------------------------------ |
| `ESA_SOURCE_URL` | `https://github.com/infinit3HQ/easy-sys-admin` | Where the link goes. Set your fork or a source archive when you ship changes.              |
| `ESA_VERSION`    | `dev`                                          | Shown next to the link.                                                                    |
| `ESA_COMMIT`     | `none`                                         | Shown next to the link. A git sha on a GitHub repository URL links to `.../tree/<commit>`. |

Set these on the web container. Compose passes them through. The deploy workflow writes `ESA_VERSION` and `ESA_COMMIT` from the commit it built.

## Upgrades

Pull the new commit and run the same `docker compose ... up -d --build`. The control plane migrates the database on start. Migrations add tables and columns. They do not drop or rename on their own.

Servers keep the helper they already have. When a release adds helper actions, reinstall the helper from each server's settings. An old helper answers a new action with `unknown action`.

Only one control plane process may run.

## Backups

Back up two things, and keep them apart:

- The PostgreSQL data.
- `ESA_MASTER_KEY`. A dump without the key loses every stored server login and channel credential.

```sh
docker compose -f deploy/compose.yaml --env-file deploy/.env exec -T postgres \
  pg_dump -U esa -d esa --format=custom > esa-$(date +%F).dump
```

Restore only after stopping the web and control plane containers. The dump has to be opened with the master key that was in use when it was taken.

There is no command that re-encrypts old secrets under a new master key. To rotate, set `ESA_MASTER_KEY` to `<new>,<old>` and restart. Keep every old key that still seals a secret.

## Current limitations

From the project's status notes:

- One control plane instance. Locks, rate limits, the notification queue, and poll back-off are in memory.
- `apt` only. No `dnf`.
- No agent mode for servers without inbound SSH.
- No live log follow. The poll interval is fixed at one minute.
- The helper protocol version is not checked.
- The audit log is not pruned.
- CLI commands for organizations, teams, and sharing are not written yet.
- On a run shared with another organization, the overall status can reflect servers that organization cannot see.
