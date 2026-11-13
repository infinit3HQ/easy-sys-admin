# The esa CLI

`esa` is one Go binary with two jobs. `esa serve` runs the control plane. Every other command is a client that talks to a running control plane over `/api/v1` with an API token, so you can check the fleet and start maintenance runs from a terminal or a script. The code lives in `apps/esa/cmd/esa` and `apps/esa/internal/cli`. The binary that runs on managed servers, `esa-helper`, is a separate program, described at the end of this page.

## Build

```sh
pnpm --filter @esa/esa build      # writes apps/esa/bin/esa and the Linux helpers
# or, from apps/esa:
go build -o bin/esa ./cmd/esa
```

`esa --version` (or `-v`) prints the version and commit set at build time. A local build prints `esa version dev (none)`.

## Log in

Create a token in the web app under Settings > API tokens, then:

```sh
esa login https://esa.example.com --token esa_...
```

`esa login <url>` does the following:

1. Adds `https://` when the URL has no scheme. Plain `http` is refused unless the host is `localhost` or `127.0.0.1`, because the token would travel in plain text.
2. Keeps only the scheme and host. Any path is dropped.
3. Takes the token from `--token`, or from `ESA_TOKEN` when the flag is empty. The token must start with `esa_`.
4. Calls `GET /api/v1/me` to check the token, then saves the login and prints the user's email, the account name, and the role.

| Flag      | Meaning                |
| --------- | ---------------------- |
| `--token` | API token (`esa_...`). |

`esa logout` deletes the saved file. It does not revoke the token. Revoke it in Settings if it may have leaked.

### Where the login is saved

The file is `$XDG_CONFIG_HOME/esa/config.json` when `XDG_CONFIG_HOME` is set, and `~/.config/esa/config.json` otherwise. macOS uses the same path as Linux. The directory is created with mode `0700` and the file with mode `0600`. It holds two fields:

```json
{
  "url": "https://esa.example.com",
  "token": "esa_..."
}
```

### Environment overrides

`ESA_URL` and `ESA_TOKEN` override the saved values one by one. With both set, no saved login is needed, which suits CI:

```sh
ESA_URL=https://esa.example.com ESA_TOKEN=esa_... esa status
```

`ESA_URL` is used as given. It does not get the `https` check that `esa login` applies, so set an `https` URL yourself.

Without a URL and a token from either source, every client command fails with `not logged in: run esa login <url> --token esa_...`.

## What a token can do

Each request runs as the token's user, in the account the token was created in. The control plane checks the user's role and the token's scopes on every call:

| Scope       | Allows                                                     | Role needed to grant it |
| ----------- | ---------------------------------------------------------- | ----------------------- |
| `read`      | Board, servers, groups, runs, alerts. Always included.     | viewer                  |
| `logs:read` | Server and container logs                                  | admin                   |
| `actions`   | Runs, service and container actions, update checks, reboot | admin                   |
| `admin`     | Servers, groups, alert rules, channels, certificates       | admin                   |

`esa runs start` needs the `actions` scope and the admin or owner role. Every other client command needs only `read`. Calls made with a token show up in the activity log with source `api`, and runs started this way have trigger `api`.

Tokens expire after 1 to 365 days. The API defaults to 90 days. The Settings page offers 30, 90, and 365.

## Global flags

| Flag              | Meaning                                                                                     |
| ----------------- | ------------------------------------------------------------------------------------------- |
| `--json`          | Print the API response as indented JSON instead of a table. `login` and `logout` ignore it. |
| `-v`, `--version` | Print the version.                                                                          |
| `-h`, `--help`    | Help for any command. `esa help <command>` works too.                                       |

## Commands

### esa status

Shows a totals line and one row per server, worst first.

```sh
esa status
```

```
12 servers: 9 ok, 1 warning, 1 failing, 1 unreachable, 0 pending

STATUS       SERVER    GROUP       CPU  MEM  DISK  UPDATES       WHY
failing      media-01  production  12%  71%  93%   3 sec / 14    disk / at 93%
...
```

`UPDATES` is security updates, then other updates. `WHY` lists the status reasons, or the last error for an unreachable server. With `--json` it prints the whole fleet overview (`totals` and `servers`).

### esa servers list

Lists servers with id, name, `host:port`, status, helper version, and when the control plane last heard from each one.

```sh
esa servers list
esa servers list --json
```

With `--json` it prints the array of servers from the fleet overview.

### esa servers sbom <name|id> and esa servers scan-import <name|id> <file>

`esa servers sbom` writes the server's installed packages as a CycloneDX SBOM to stdout. Run any scanner on it, wherever you like, then import a Grype JSON report with `esa servers scan-import`. esa sends the SBOM nowhere. `scan-import` keeps only the fields esa reads before it uploads, so a large report fits the 10 MiB limit (a real 69 MB report became 5 MB).

```sh
esa servers sbom web-01 > web-01.cdx.json
grype sbom:web-01.cdx.json -o json > web-01.grype.json
esa servers scan-import web-01 web-01.grype.json
```

### esa servers show <name|id>

Shows one server's latest snapshot: status reasons, OS, kernel, uptime, CPU, load, memory, a table of mounts, and the pending update count.

```sh
esa servers show media-01
esa servers show 42 --json
```

The argument matches a server name (case-insensitive) or a numeric id. With `--json` it prints the full server detail, including the snapshot and pending updates.

### esa groups list

Lists groups with id, name, server count, cleanup schedule, update schedule, and timezone. A schedule that is off shows `off`.

```sh
esa groups list
```

### esa runs list

Lists recent runs, newest first, with id, kind, target, status, start time, and summary.

| Flag      | Default | Meaning                                  |
| --------- | ------- | ---------------------------------------- |
| `--limit` | `20`    | How many runs. The API accepts 1 to 200. |

```sh
esa runs list --limit 50
```

### esa runs show <id>

Shows one run and each server's result: status, bytes freed, and error.

```sh
esa runs show 118
```

The id must be a number.

### esa runs start <measure|clean|update|reboot>

Starts a run on a group or on a list of servers.

| Flag             | Meaning                                                                                                                    |
| ---------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `--group`        | Group name (case-insensitive) or numeric id.                                                                               |
| `--server`       | Server name or id. Repeat the flag or separate values with commas.                                                         |
| `--wait`         | Poll every 3 seconds until the run finishes, then print the result. Progress dots go to stderr.                            |
| `--mode`         | For `update` runs only: `security` or `all`. Without it the run uses the group's setting.                                  |
| `--batch`        | For `update` and `reboot` runs: servers worked on at the same time, 1 to 50. Without it the run uses the group's setting.  |
| `--max-failures` | For `update` and `reboot` runs: stop after this many servers fail, 1 to 1000. Without it the run uses the group's setting. |

Give exactly one of `--group` or `--server`. All servers in one run must belong to one organization.

```sh
esa runs start measure --group production
esa runs start clean --server media-01 --wait
esa runs start clean --server web-01,web-02
esa runs start update --group production --mode security
esa runs start update --group production --batch 10 --max-failures 3
esa runs start reboot --server 42 --wait
```

What the kinds do:

- `measure` reports how much space a cleanup would free. It changes nothing.
- `clean` prunes idle Docker build cache, removes old VS Code server copies, and vacuums the journal, as the group's settings allow. A run on servers without `--group` cleans build cache and VS Code copies and leaves the journal alone.
- `update` installs package updates in batches: the servers of a batch upgrade at the same time, and the next batch starts when the batch is done. The run stops once the failure limit is reached, and the remaining servers are skipped. The server that hosts the control plane always goes in a batch of its own.
- `reboot` reboots and waits for the server to come back, in batches the same way.

Without `--wait`, the command prints the run id and exits. With `--json`, it prints the run as the API returned it.

`--mode`, `--batch`, and `--max-failures` change only what they set. With `--group`, the CLI reads the group's settings first (cleanups, journal size, update mode, held packages, reboot if required, batch size, failure limit) and sends them with the flags applied, so the run keeps the group's held packages and reboot setting. With `--server`, the rest are the control plane's defaults: update mode `security`, no held packages, no reboot, batch size 1, failure limit 1. `--mode` is ignored for kinds other than `update`, and `--batch` and `--max-failures` for kinds other than `update` and `reboot`.

With the default batch size of 1 and failure limit of 1, a run works on one server at a time and stops at the first failure. For a group of 200 servers at about 3 minutes each, that takes about 10 hours; `--batch 10` takes about 1 hour. A larger batch spreads a bad update to more servers before the run stops, because the servers already running in a batch always finish.

### esa plans

A group can require an approved plan. Then `esa runs start clean` and `esa runs start update` are refused on it. Propose a plan, have a person approve it on the Plans page of the web app, then run it. There is no approve command: a token cannot approve.

| Command                                                | What it does                                                                                                                                                                                                                                                                                                    |
| ------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `esa plans list`                                       | The 100 most recent plans with kind, target, status, and run.                                                                                                                                                                                                                                                   |
| `esa plans show <id>`                                  | One plan: its servers, update mode, held packages, reboot setting, cleanups, and the pending packages it covers.                                                                                                                                                                                                |
| `esa plans propose <measure\|clean\|update> --group G` | Stores a plan with the group's settings. `--server` (repeatable) instead of `--group` targets servers. `--mode security\|all` changes the update mode. `--package P` (repeatable) approves only those pending packages; the helper leaves the rest alone. Needs `actions` and not `logs:read`. Changes nothing. |
| `esa plans cancel <id> [--reason R]`                   | Withdraws a plan that has not started. It never touches a server.                                                                                                                                                                                                                                               |
| `esa plans execute <id>`                               | Starts an approved plan within its 30 minutes. A repeat prints the same run.                                                                                                                                                                                                                                    |

A plan covers the servers in the group when it was proposed. A server added to or removed from the group, or a change in the pending packages, cancels the approval.

```sh
esa plans propose update --group production
esa plans show 12
esa plans execute 12
```

### esa alerts

Lists open alerts with severity, kind, message, and when each one fired (or started pending). Prints `No open alerts.` when there are none.

| Flag    | Meaning                      |
| ------- | ---------------------------- |
| `--all` | Include resolved alerts too. |

```sh
esa alerts
esa alerts --all --json
```

### esa completion <bash|zsh|fish|powershell>

Prints a shell completion script. Cobra adds this command. Run `esa completion <shell> --help` for install steps.

```sh
esa completion zsh > "${fpath[1]}/_esa"
```

## Name resolution

`servers show`, `runs start --server`, and `runs start --group` accept a name or an id. The CLI compares names without case and takes the first match: for servers in fleet overview order (worst first), for groups in list order. Names are unique inside one account only. A group shared by another organization can have the same name as one of yours, so use the numeric id when names repeat. `esa servers list` and `esa groups list` show the ids.

## Exit codes

| Code | When                                                                               |
| ---- | ---------------------------------------------------------------------------------- |
| `0`  | The command succeeded.                                                             |
| `1`  | Any error: bad arguments, not logged in, an API error, a network error, or Ctrl-C. |

Errors print to stderr as `esa: <message>`. API errors show the control plane's message, prefixed with the field name for validation errors. Ctrl-C exits with `1` and prints nothing.

`esa runs start --wait` exits `1` when the run ends with any status other than `succeeded`, such as `partial` or `failed`. With `--json` added, it prints the run and exits `0` whatever the status, so check the `status` field in the JSON instead.

## esa serve

Runs the control plane: the JSON API under `/api/v1`, sign-in under `/auth`, enroll scripts under `/enroll`, `/healthz`, and the background jobs that poll servers and run schedules. It takes no arguments and reads everything from the environment:

| Variable                                           | Default                            | Meaning                                                                                                                                      |
| -------------------------------------------------- | ---------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `ESA_DATABASE_URL`                                 | required                           | PostgreSQL URL.                                                                                                                              |
| `ESA_MASTER_KEY`                                   | required                           | 32 random bytes, base64 (`openssl rand -base64 32`). Comma-separate several to rotate. The first seals new secrets.                          |
| `ESA_LISTEN`                                       | `127.0.0.1:8080`                   | Bind address.                                                                                                                                |
| `ESA_PUBLIC_URL`                                   | `http://localhost:3000`            | Origin users open. Must be an absolute `http` or `https` URL without a path.                                                                 |
| `ESA_HELPER_DIR`                                   | `./bin`                            | Directory with `esa-helper-linux-amd64` and `esa-helper-linux-arm64`.                                                                        |
| `ESA_LOG_FORMAT`                                   | `text`                             | `text` or `json`. Logs go to stderr.                                                                                                         |
| `ESA_AUTH_MODE`                                    | `standard`                         | `standard` (`oidc` means the same) or `dev`. `dev` signs everyone in as one local user and needs loopback `ESA_LISTEN` and `ESA_PUBLIC_URL`. |
| `ESA_AUTH_PASSWORD`                                | `true`                             | Email and password sign-in.                                                                                                                  |
| `ESA_SIGNUP`                                       | `false`                            | Open email and password sign-up. Turned off, with a warning, when `ESA_ALLOWED_EMAILS` is set.                                               |
| `ESA_ALLOWED_EMAILS`                               | empty                              | Comma-separated allow list for every sign-in method. Empty allows everyone.                                                                  |
| `ESA_SMTP_HOST`                                    | empty                              | System SMTP for confirmation and password reset. Empty leaves both off. See [configuration.md](configuration.md).                            |
| `ESA_OIDC_ISSUER`                                  | empty                              | OpenID Connect issuer. Needs `ESA_OIDC_CLIENT_ID` and `ESA_OIDC_CLIENT_SECRET`.                                                              |
| `ESA_OIDC_PROVIDER_NAME`                           | `Authentik`                        | Name on the sign-in button.                                                                                                                  |
| `ESA_GOOGLE_CLIENT_ID`, `ESA_GOOGLE_CLIENT_SECRET` | empty                              | Google sign-in.                                                                                                                              |
| `ESA_ALLOW_PRIVATE_NETWORKS`                       | `true`, or `false` with sign-up on | Allow private and loopback addresses for servers, certificate checks, and channels.                                                          |

Startup fails when no sign-in method is on: password sign-in, an OIDC provider, Google, or dev mode.

```sh
ESA_DATABASE_URL=postgres://esa:secret@localhost:5432/esa \
ESA_MASTER_KEY=$(openssl rand -base64 32) \
esa serve
```

For local development, `pnpm --filter @esa/esa dev` builds the helpers and runs `go run ./cmd/esa serve` with the repo's `.env`.

## esa reseal

Re-seals stored secrets with the first key in `ESA_MASTER_KEY`. It reads the database directly. It is not an API call and it does not print key material.

```sh
esa reseal --dry-run
esa reseal
```

Stop `esa serve` first so it is not writing secrets at the same time. The command covers account SSH keys, saved server logins, and notification channel settings. `--dry-run` writes nothing. See [security.md](security.md#rotating-the-master-key).

### esa healthcheck

A hidden command for the container's `HEALTHCHECK`, since the image has no shell or curl. It calls `/healthz` on `ESA_LISTEN` (using `127.0.0.1` when the host is empty, `0.0.0.0`, or `::`) with a 3 second timeout and exits `0` on HTTP 200, `1` otherwise.

## esa-helper

`esa-helper` (`apps/esa/cmd/esa-helper`) is installed on each managed server at `/usr/local/lib/esa/esa-helper`. The control plane runs it over SSH as `sudo -n /usr/local/lib/esa/esa-helper <action> [flags]`. Each action prints one JSON envelope on stdout and exits `0` on success, `1` on failure. You do not need to run it by hand. Running it with no action or `--help` prints the action list and exits `2`.

Read actions: `version`, `status`, `updates [--refresh]`, `upgrade-precheck`, `upgrade-status --run-id N`, `services`, `containers [--protect a,b]`, `image-check`, `logs (--unit U | --container C) [--lines N] [--priority P] [--grep S]`, `access`, `security`, `jobs`, `clean --measure [--only ids] [--journal-size 200M]`.

Write actions: `clean --apply [--only buildcache,vscode,journal] [--journal-size 200M]`, `upgrade-start --run-id N --mode security|all [--hold pkg,pkg]`, `service --unit U --action start|stop|restart`, `container --name C --action start|stop|restart [--confirm NAME] [--protect a,b]`, `container-update --name C [--confirm NAME] [--protect a,b]`, `reboot`, `uninstall`.

Every action except `version` must run as root. `ssh-dispatch` is the forced command on the esa SSH key. It runs as the esa user, accepts only `sudo -n /usr/local/lib/esa/esa-helper <action> ...` from `SSH_ORIGINAL_COMMAND`, and hands that to sudo.
