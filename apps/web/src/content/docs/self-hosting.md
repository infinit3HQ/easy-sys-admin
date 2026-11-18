---
title: Self-hosting
description: Install public container images or build from source, with PostgreSQL, TLS, authentication, upgrades, and backups.
---

You can self-host from source or use the public container images in GitHub Container Registry. Both use the AGPL-3.0-only source. Features follow the source revision you choose. Docker images support Linux amd64 and arm64.

## Choose a build

| Channel | Control plane                                                       | Web                                                       | Use                                                                                         |
| ------- | ------------------------------------------------------------------- | --------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| Stable  | `ghcr.io/infinit3hq/easy-sys-admin-control-plane:stable`            | `ghcr.io/infinit3hq/easy-sys-admin-web:stable`            | Builds from `main` after all checks pass.                                                   |
| Nightly | `ghcr.io/infinit3hq/easy-sys-admin-control-plane:nightly`           | `ghcr.io/infinit3hq/easy-sys-admin-web:nightly`           | Preview builds from `nightly`. Use a separate test database.                                |
| Commit  | `ghcr.io/infinit3hq/easy-sys-admin-control-plane:sha-<full-commit>` | `ghcr.io/infinit3hq/easy-sys-admin-web:sha-<full-commit>` | A matching production source revision. Nightly commit tags use `nightly-sha-<full-commit>`. |

`latest` follows `stable`. Channel tags move. For a reproducible installation or rollback, copy the exact image digests from the successful GitHub Actions run and keep both image references together. The workflow publishes channel aliases only after both images build. Stable tags appear after the first successful `main` publication; nightly tags appear after the first successful nightly publication.

Public images need no registry login. Source, revision, and licence labels are attached to both images. Builds include provenance and SBOM attestations. The control-plane image includes the helper for both server architectures. Releases that support outbound transport also include the agent.

Self-hosting has no licence fee or usage cap. Leave `ESA_BETA_LIMITS` unset. The default operational cap is 500 servers per account and 20 organizations per person. `ESA_MAX_SERVERS_PER_ACCOUNT` changes the server cap.

## Requirements

- A Linux host with Docker Engine and Compose 2.24.4 or newer. Building from source uses the Dockerfiles; a binary install needs Go, Node, and pnpm too.
- PostgreSQL 18. Compose supplies it with a separate data volume and an internal database network.
- One hostname and TLS. The Compose stack expects an existing Traefik proxy on a Docker network named `proxy`, an entrypoint `websecure`, and a certificate resolver `cloudflare`. These names are configurable.
- A 32-byte `ESA_MASTER_KEY`, from `openssl rand -base64 32`. Back it up separately from the database.
- Outgoing SSH for SSH-managed hosts, or outgoing HTTPS from hosts using the [outbound agent](/outbound-agent).

Managed hosts use Linux amd64 or arm64 and `sudo`. Package operations support APT on Debian/Ubuntu and DNF on Rocky Linux 9 with `dnf-utils`. Other RHEL-family releases are not supported.

## Configure Docker Compose

Clone the repository to obtain the Compose files and example settings. No Go or Node build tools are needed when using published images.

```sh
git clone https://github.com/infinit3HQ/easy-sys-admin.git
cd easy-sys-admin
cp deploy/.env.example deploy/.env
chmod 600 deploy/.env
docker network create proxy  # skip when the existing proxy network is present
```

Set these in `deploy/.env`:

```ini
ESA_HOST=esa.example.com
ESA_PUBLIC_URL=https://esa.example.com
ESA_MASTER_KEY=your-generated-key
POSTGRES_PASSWORD=your-generated-hex-password
ESA_AUTH_PASSWORD=true
ESA_SIGNUP=false
ESA_CONTROL_PLANE_IMAGE=ghcr.io/infinit3hq/easy-sys-admin-control-plane:stable
ESA_WEB_IMAGE=ghcr.io/infinit3hq/easy-sys-admin-web:stable
```

Generate the database password with `openssl rand -hex 24`. Do not copy these placeholder credentials. Set `TRAEFIK_NETWORK`, `TRAEFIK_ENTRYPOINT`, and `TRAEFIK_CERT_RESOLVER` if your proxy uses different names. Keep `ESA_SEO=false` for private installations.

Create the first account before sharing access to the hostname. With sign-up off, the first user can still initialize an empty installation. To add more people while sign-up stays closed, configure a verified identity provider. `ESA_ALLOWED_EMAILS` restricts sign-in; its presence also disables general password registration.

## Use published images

```sh
docker compose -f deploy/compose.yaml -f deploy/compose.images.yaml --env-file deploy/.env pull
docker compose -f deploy/compose.yaml -f deploy/compose.images.yaml --env-file deploy/.env up -d --no-build --wait
docker compose -f deploy/compose.yaml -f deploy/compose.images.yaml --env-file deploy/.env ps
curl -fsS https://esa.example.com/healthz
```

The image override removes every local build section. PostgreSQL, the control plane, and the web app start in dependency order. Only the web service joins Traefik. Neither PostgreSQL nor port 8080 is published. One hostname serves the board, API, auth, MCP, and helper downloads.

## Build from source

Use a tested source commit and build locally:

```sh
# Keep the same configuration, but use local image names.
# Remove ESA_CONTROL_PLANE_IMAGE and ESA_WEB_IMAGE from deploy/.env.
docker compose -f deploy/compose.yaml --env-file deploy/.env up -d --build --wait
```

The source path uses the Dockerfiles in `apps/esa` and `apps/web`. Build tools run inside the build containers. To build binaries outside Docker, use Node 26, pnpm 12, Go 1.27, and PostgreSQL:

```sh
pnpm install --frozen-lockfile
pnpm --filter @esa/esa build
pnpm --filter @esa/web build
```

Run `apps/esa/bin/esa serve` with `ESA_DATABASE_URL`, `ESA_MASTER_KEY`, `ESA_PUBLIC_URL`, and `ESA_HELPER_DIR=./apps/esa/bin`. Start the built web app with `ESA_API_URL=http://127.0.0.1:8080`, `HOST=127.0.0.1`, and `pnpm --filter @esa/web start`. Put TLS in front of the web app. Port 8080 also has unauthenticated operational metrics and must stay private.

## Authentication and source offer

Google redirects to `$ESA_PUBLIC_URL/auth/callback/google`. A generic OIDC provider redirects to `$ESA_PUBLIC_URL/auth/callback/oidc`. Configure the corresponding client ID and secret. Password confirmation and reset need the SMTP settings; leaving `ESA_SMTP_HOST` empty sends no auth mail.

`ESA_SOURCE_URL` defaults to the upstream repository. Set it to your fork when you run modified code. `ESA_VERSION` and `ESA_COMMIT` identify the running source in the footer. Published web images carry their build revision; the deployment workflow also passes the exact revision explicitly. The AGPL source offer applies to modified builds served over a network.

## Upgrades and rollbacks

Back up the database and master key before upgrading. Check schema compatibility before rolling back to an older image. Restore a matching backup when required. For image installs, update both image references to one tested revision, pull, then run the same `up -d --no-build --wait` command. For source installs, check out the tested commit and rebuild. The control plane applies additive migrations on startup.

An older image is not automatically a safe database rollback. Retain a pre-upgrade database dump and the keys used to encrypt its data. Restore a matching snapshot when schema compatibility has not been established. Do not restore or reuse a production database in nightly.

Managed-server helpers are updated only by an explicit installation. New actions are refused until the installed helper advertises them. Compatible older helpers can still run supported actions.

## Backups and operational settings

```sh
docker compose -f deploy/compose.yaml --env-file deploy/.env exec -T postgres \
  pg_dump -U esa -d esa --format=custom > esa-$(date +%F).dump
```

Keep the dump and `ESA_MASTER_KEY` separately. A dump without its key cannot recover stored credentials. Stop the web and control plane before restoring.

Multiple control planes coordinate maintenance and jobs through PostgreSQL. Keep their versions compatible and finish upgrading every instance before enabling new features. Master-key rotation supports `esa reseal`, including a dry run; remove an old key only when all live data has been re-sealed. Optional audit/report retention and retention holds are deployment environment variables. They default off. See the repository's `docs/configuration.md` for the complete list.
