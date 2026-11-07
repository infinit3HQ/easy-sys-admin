# Deployment

easy-sys-admin runs on one Docker host behind Traefik, using `deploy/compose.yaml`. The stack has three containers: PostgreSQL, the Go control plane, and the web server. No ports are published. Traefik reaches the web container over its Docker network, and one hostname serves the board, the API (`/api/v1`), sign-in (`/auth`), MCP (`/mcp`), and the enroll scripts (`/enroll`). This page covers a deployment by hand, a deployment with GitHub Actions on a self-hosted runner, upgrades, backups, health checks, monitoring, and rollbacks. The examples use `esa.example.com`. Every setting is described in [configuration.md](configuration.md).

## How the stack is wired

| Container  | Networks                    | Role                                                                                                                                                             |
| ---------- | --------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `postgres` | `database`                  | PostgreSQL 18. Data lives in the named volume `postgres`. The `database` network is internal, so the container has no outside access.                            |
| `esa`      | `internal`, `database`      | The control plane on port 8080. It is the only container that talks to PostgreSQL. It needs normal outbound access to reach managed servers over SSH.            |
| `web`      | `internal`, the Traefik one | The web server on port 3000. Traefik routes the hostname to it. It serves the board and `/mcp`, and proxies `/api`, `/auth`, and `/enroll` to `http://esa:8080`. |

Compose starts the containers in order. The control plane waits for PostgreSQL to pass `pg_isready`, and the web server waits for the control plane to pass its health check.

## Deploy with docker compose

You need a Linux host with Docker Engine and the Compose plugin, and a DNS record for your hostname that points at the host.

1. Run Traefik on the host with the Docker provider. It needs an entrypoint with TLS (the compose file assumes `websecure`) and a certificate resolver (it assumes `cloudflare`). Attach Traefik to an external Docker network, by default `proxy`:

   ```sh
   docker network create proxy
   ```

   If your names differ, set `TRAEFIK_NETWORK`, `TRAEFIK_ENTRYPOINT`, and `TRAEFIK_CERT_RESOLVER` in step 4.

2. Clone the repository on the host.

3. Create the env file and make it readable only by you:

   ```sh
   cp deploy/.env.example deploy/.env
   chmod 600 deploy/.env
   ```

4. Fill in `deploy/.env`:
   - `ESA_HOST=esa.example.com`
   - `ESA_PUBLIC_URL=https://esa.example.com`
   - `ESA_MASTER_KEY`, from `openssl rand -base64 32`. Store a copy outside the host. Without it, stored server logins and channel credentials cannot be read.
   - `POSTGRES_PASSWORD`, from `openssl rand -hex 24`. Compose puts it into a database URL, so avoid characters such as `@`, `/`, `:`, and `#`.
   - The sign-in settings you want. Password sign-in is on by default. To use OIDC or Google, register `https://esa.example.com/auth/callback/oidc` or `https://esa.example.com/auth/callback/google` as the redirect URI with the provider.

5. Build and start the stack:

   ```sh
   docker compose -f deploy/compose.yaml --env-file deploy/.env up -d --build
   ```

6. Check that it is healthy:

   ```sh
   docker compose -f deploy/compose.yaml --env-file deploy/.env ps
   curl -fsS https://esa.example.com/healthz
   ```

7. Open `https://esa.example.com` and create the first account. The first account can always be created while the database has no users, even with `ESA_SIGNUP=false`.

The control plane connects out to the SSH port of every server you add. When an enrollment completes, it also connects to the new server to confirm its host key. Make sure the host's firewall allows that outbound traffic.

## Deploy with GitHub Actions

`.github/workflows/deploy.yml` builds the images on the host and restarts the stack with compose. It runs on a self-hosted runner on that host.

### When it runs

- After the `CI` workflow (`.github/workflows/ci.yml`) completes on `main`. The `workflow_run` trigger deploys only when CI succeeded, CI was started by a push to `main` of this repository, and the repository variable `DEPLOY_ENABLED` is `true`. CI runs for pull requests and forks never deploy.
- On demand with "Run workflow" (`workflow_dispatch`). The job runs only when the selected branch is `main`, and it does not check `DEPLOY_ENABLED`.

Deploys share the concurrency group `deploy-production`. A new deploy waits for a running one to finish instead of cancelling it.

### What the job does

1. Checks out the commit CI tested (`workflow_run.head_sha`), or the head of `main` for a manual run. It does not keep Git credentials.
2. Checks that `ESA_HOST`, `ESA_MASTER_KEY`, and `POSTGRES_PASSWORD` are set, and that no value contains a single quote or a line break.
3. Writes the env file to `$RUNNER_TEMP/esa-$GITHUB_RUN_ID.env` with mode `0600`. It adds `ESA_PUBLIC_URL=https://$ESA_HOST`, `ESA_VERSION` (the short commit hash), and `ESA_COMMIT` (the full hash).
4. Runs `docker compose build --pull`, then `docker compose up -d --remove-orphans --wait --wait-timeout 180`.
5. Prints `docker compose ps` and fetches `http://127.0.0.1:3000/healthz` inside the web container.
6. Deletes the env file, whether the job passed or failed.

### Set up the repository

1. In Settings > Environments, create an environment named `production`. Under "Deployment branches and tags", choose "Selected branches and tags" and add `main`. This keeps the environment's secrets away from every other branch. You can also add a required reviewer. On private repositories that needs a paid GitHub plan.
2. On the `production` environment, add secrets for anything that must stay hidden:
   - `ESA_MASTER_KEY` (required)
   - `POSTGRES_PASSWORD` (required)
   - `ESA_OIDC_CLIENT_SECRET` (with OIDC)
   - `ESA_GOOGLE_CLIENT_SECRET` (with Google)
   - `ESA_SMTP_PASSWORD` (when system mail is on)

   Keep them as environment secrets, not repository secrets, so only jobs that deploy from `main` can read them. A secret is masked in logs and cannot be read back from GitHub.

3. On the same `production` environment, add variables for the rest. These are visible in the environment settings, which is fine because they are not credentials:
   - `ESA_HOST`, for example `esa.example.com` (required)
   - Any of `ESA_SIGNUP`, `ESA_AUTH_PASSWORD`, `ESA_ALLOW_PRIVATE_NETWORKS`, `ESA_ALLOWED_EMAILS`, `ESA_OIDC_ISSUER`, `ESA_OIDC_PROVIDER_NAME`, `ESA_OIDC_CLIENT_ID`, `ESA_GOOGLE_CLIENT_ID`, `ESA_SMTP_HOST`, `ESA_SMTP_PORT`, `ESA_SMTP_USERNAME`, `ESA_SMTP_FROM`, `ESA_POLL_CONCURRENCY`, `ESA_MAX_SERVERS_PER_ACCOUNT`, `ESA_BETA_LIMITS`, `ESA_SEO`, `ESA_METRICS_RAW_RETENTION`, `ESA_METRICS_HOURLY_RETENTION`, `ESA_STACK_NAME`, `ESA_ROUTER`, `TRAEFIK_NETWORK`, `TRAEFIK_ENTRYPOINT`, and `TRAEFIK_CERT_RESOLVER`. Unset ones get the defaults in [configuration.md](configuration.md). Set `ESA_BETA_LIMITS` to `true` for the hosted beta. Leave it unset for a self-hosted install. Set `ESA_SEO` to `true` only for a site that should appear in search engines. Leave `ESA_SMTP_HOST` empty to send no auth mail.

4. Add one repository variable, `DEPLOY_ENABLED=true`, once you want pushes to `main` to deploy. This one cannot live on the environment. The workflow checks it before GitHub selects the `production` environment, so an environment variable would be invisible there.

5. Set up Traefik on the host as in step 1 of [Deploy with docker compose](#deploy-with-docker-compose).

### Set up the self-hosted runner

Run the runner under its own user so a job cannot touch other users' files. The user needs the `docker` group to run compose. Membership in that group gives root access to the host through Docker, so use this runner only for this repository's deploy workflow.

1. Create the user and add it to the `docker` group:

   ```sh
   sudo useradd --create-home --shell /bin/bash github-runner
   sudo usermod -aG docker github-runner
   ```

2. Open the repository's Settings > Actions > Runners > New self-hosted runner, and choose Linux and your architecture. GitHub shows the download commands and a registration token.

3. As the new user, download and extract the runner with those commands, then register it with the `esa-deploy` label:

   ```sh
   sudo -iu github-runner
   mkdir actions-runner && cd actions-runner
   # download and extract the runner here, using the commands GitHub shows
   ./config.sh --url https://github.com/OWNER/REPO --token REGISTRATION_TOKEN --labels esa-deploy
   exit
   ```

   The runner gets the `self-hosted` label on its own. The workflow asks for both `self-hosted` and `esa-deploy`.

4. Install and start the runner as a service that runs as that user:

   ```sh
   cd /home/github-runner/actions-runner
   sudo ./svc.sh install github-runner
   sudo ./svc.sh start
   ```

5. Push to `main`, or use "Run workflow" on the Deploy workflow, and watch the job.

The env file only exists while a job runs. To run compose commands on the host later, either write a temporary env file or use plain `docker` commands with the container names, which are `<ESA_STACK_NAME>-<service>-1`, for example `esa-postgres-1`.

## Upgrade

- With GitHub Actions, push to `main`. The deploy runs after CI passes. You can also use "Run workflow", or re-run the latest Deploy run.
- By hand, pull the new commit and run the same `docker compose ... up -d --build` command as for the first deploy.

The control plane migrates the database every time it starts. It takes a PostgreSQL advisory lock first, so two control planes that start at once do not migrate at the same time. Migrations use GORM AutoMigrate on the models in `apps/esa/internal/store/models.go`. AutoMigrate adds tables, columns, indexes, and constraints. It never drops or renames anything. Drops, renames, and data fixes are hand-written steps in `postMigrate` in `apps/esa/internal/store/migrate.go`, and each one must be safe to run on every start.

Servers keep their installed helper after an upgrade. When a release adds helper actions, reinstall the helper from each server's settings tab; an old helper answers a new action with `unknown action`.

## Back up

Back up two things, and keep them in separate places:

- The PostgreSQL data, in the volume `<ESA_STACK_NAME>_postgres` (`esa_postgres` by default).
- `ESA_MASTER_KEY`. A database backup without the key loses every stored server login and channel credential.

Take a logical dump while the stack runs:

```sh
docker compose -f deploy/compose.yaml --env-file deploy/.env exec -T postgres \
  pg_dump -U esa -d esa --format=custom > esa-$(date +%F).dump
```

On a host deployed by GitHub Actions there is no env file, so call the container directly:

```sh
docker exec esa-postgres-1 pg_dump -U esa -d esa --format=custom > esa-$(date +%F).dump
```

To restore a dump, stop the control plane and the web server first, restore, then start them again:

```sh
docker compose -f deploy/compose.yaml --env-file deploy/.env stop web esa
docker compose -f deploy/compose.yaml --env-file deploy/.env exec -T postgres \
  pg_restore -U esa -d esa --clean --if-exists < esa-2026-01-31.dump
docker compose -f deploy/compose.yaml --env-file deploy/.env start esa web
```

The restored data needs the `ESA_MASTER_KEY` that was in use when the dump was taken.

## Health check

The web server answers `GET /healthz`:

```sh
curl -fsS https://esa.example.com/healthz
```

| Status | Body                                                 | Meaning                                              |
| ------ | ---------------------------------------------------- | ---------------------------------------------------- |
| 200    | `{"web":"ok","controlPlane":"ok"}`                   | Both servers are up and the database answers.        |
| 503    | `{"web":"ok","controlPlane":"database unavailable"}` | The control plane is up but cannot reach PostgreSQL. |
| 503    | `{"web":"ok","controlPlane":"unreachable"}`          | The web server cannot reach the control plane.       |

Each container also has a Docker health check. PostgreSQL runs `pg_isready`, the control plane runs `esa healthcheck` against its own `/healthz`, and the web server fetches `/`. `docker compose ps` shows the results.

## Monitoring

The control plane serves `GET /metrics` in the Prometheus text format on port 8080. It needs no login. The port is only on the stack's `internal` Docker network (and on loopback when you run `esa serve` yourself), and the web server does not proxy `/metrics`, so it cannot be reached through the public hostname. The output holds counts only, never server names, hosts, emails, or tokens. Set `ESA_METRICS=false` to turn it off.

| Metric                           | Type      | Meaning                                                                                  |
| -------------------------------- | --------- | ---------------------------------------------------------------------------------------- |
| `esa_build_info{version,commit}` | gauge     | Always 1. The labels say what is running.                                                |
| `esa_db_up`                      | gauge     | 1 when PostgreSQL answered the scrape.                                                   |
| `esa_servers{status}`            | gauge     | Servers in all accounts by status: `ok`, `warning`, `failing`, `unreachable`, `pending`. |
| `esa_runs_running`               | gauge     | Maintenance runs in progress.                                                            |
| `esa_poll_cycle_seconds`         | gauge     | Duration of the last poll cycle.                                                         |
| `esa_poll_cycle_servers`         | gauge     | Ready servers at the start of the last cycle.                                            |
| `esa_poll_cycle_skipped`         | gauge     | Servers the last cycle had no time to start.                                             |
| `esa_polls_total{result}`        | counter   | Scheduled polls: `ok`, `failed`, and `deferred` (skipped by back-off).                   |
| `esa_poll_duration_seconds`      | histogram | Time per scheduled poll, buckets 0.5, 1, 2, 5, 10, 30, and 45 seconds.                   |
| `esa_ssh_connections`            | gauge     | Open pooled SSH connections.                                                             |
| `esa_notify_queue_length`        | gauge     | Alert notifications waiting for delivery.                                                |
| `esa_notify_dropped_total`       | counter   | Notifications dropped because the queue was full.                                        |

Check it from the host:

```sh
docker compose -f deploy/compose.yaml --env-file deploy/.env exec -T web wget -qO- http://esa:8080/metrics
```

To scrape it, run Prometheus on the same Docker network. Add the stack's `internal` network (`<ESA_STACK_NAME>_internal`, `esa_internal` by default) to the Prometheus container as an external network, then point a job at the `esa` service:

```yaml
scrape_configs:
  - job_name: esa
    scrape_interval: 30s
    static_configs:
      - targets: ['esa:8080']
```

Two signs that polling needs attention:

- `esa_poll_cycle_skipped` above 0, or the log line `poll cycle ran out of time; the rest wait for the next cycle` at warning level. It names how many servers were skipped and how many there are. Raise `ESA_POLL_CONCURRENCY` when it repeats.
- `esa_poll_cycle_seconds` close to 55. The next cycle starts a minute after the last one, so a cycle near its limit has no room left.

## Roll back

The schema only grows, so older code usually runs on a newer database. Before you roll back, check the commits you are rolling back over for changes to `apps/esa/internal/store/models.go`. A new `NOT NULL` column without a default, or a `postMigrate` step that changed data, can break the older code. In that case, restore the backup taken before the upgrade.

With GitHub Actions you have two ways:

- Re-run an older Deploy run. Open Actions > Deploy, pick the run of the commit you want, and choose "Re-run all jobs". A re-run uses the original event, so it checks out that run's commit. GitHub allows re-runs for 30 days after the original run.
- Revert the bad commit on `main` with `git revert` and push. CI runs and the revert deploys like any other push.

"Run workflow" always deploys the head of `main`. It has no input for an older commit, so it cannot roll back on its own.

By hand, check out the older commit on the host and run the compose command again:

```sh
git checkout <commit>
docker compose -f deploy/compose.yaml --env-file deploy/.env up -d --build
```
