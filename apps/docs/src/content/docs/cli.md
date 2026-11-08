---
title: CLI
description: esa login, status, servers, groups, runs, and alerts.
---

`esa` is one program with two jobs. `esa serve` runs the control plane. Every other command talks to a control plane you already have, including the hosted beta, using an API token.

Published downloads are coming soon. Until then, build it from the source tree:

```sh
pnpm --filter @esa/esa build
# binary: apps/esa/bin/esa
```

`esa --version` prints the version and commit baked in at build time. A local build prints `esa version dev (none)`.

## Log in

Create a token under Settings, API tokens. It must start with `esa_`.

```sh
esa login https://easysysadmin.app --token esa_...
```

`esa login` adds `https://` when you omit the scheme. Plain `http` is refused unless the host is `localhost` or `127.0.0.1`. A path on the URL is dropped. The token comes from `--token` or, if that is empty, from `ESA_TOKEN`. The command calls `GET /api/v1/me`, then saves the URL and the token.

`esa logout` deletes the saved file. It does not revoke the token. Revoke it under Settings if it may have leaked.

The file is `$XDG_CONFIG_HOME/esa/config.json` when that variable is set, and `~/.config/esa/config.json` otherwise. macOS uses the same path. The directory is mode `0700` and the file is mode `0600`.

`ESA_URL` and `ESA_TOKEN` override the saved values one by one. With both set, you do not need a saved login:

```sh
ESA_URL=https://easysysadmin.app ESA_TOKEN=esa_... esa status
```

`ESA_URL` is used as given. It does not get the `https` check that `esa login` applies.

Without a URL and a token, client commands fail with `not logged in: run esa login <url> --token esa_...`.

A token acts as you, in the space where it was created. `esa runs start` needs the `actions` scope. The other client commands need `read`. Calls show up in the activity log with source `api`.

## Global flags

| Flag              | Meaning                                                                  |
| ----------------- | ------------------------------------------------------------------------ |
| `--json`          | Print the API response as indented JSON. `login` and `logout` ignore it. |
| `-v`, `--version` | Print the version.                                                       |
| `-h`, `--help`    | Help. `esa help <command>` works too.                                    |

## Commands

### esa status

One totals line, then one row per server, worst first. `UPDATES` is security updates, then other updates. `WHY` is the status reason, or the last error when the server is unreachable.

```sh
esa status
```

### esa servers list

Id, name, host and port, status, helper version, and when the control plane last heard from the server.

```sh
esa servers list
```

### esa servers show

Latest snapshot for one server: status reasons, OS, kernel, uptime, CPU, load, memory, mounts, and the pending update count. The argument is a name, compared without case, or a numeric id.

```sh
esa servers show media-01
esa servers show 42 --json
```

### esa groups list

Id, name, server count, cleanup schedule, update schedule, and timezone. A schedule that is off shows `off`.

```sh
esa groups list
```

### esa runs list

Recent runs, newest first.

| Flag      | Default | Meaning                                  |
| --------- | ------- | ---------------------------------------- |
| `--limit` | `20`    | How many runs. The API accepts 1 to 200. |

```sh
esa runs list --limit 50
```

### esa runs show

One run and each server's result: status, bytes freed, and error. The id is a number.

```sh
esa runs show 118
```

### esa runs start

```sh
esa runs start <measure|clean|update|reboot>
```

| Flag             | Meaning                                                                                                             |
| ---------------- | ------------------------------------------------------------------------------------------------------------------- |
| `--group`        | Group name, without case, or a numeric id.                                                                          |
| `--server`       | Server name or id. Repeat the flag, or separate values with commas.                                                 |
| `--wait`         | Poll every 3 seconds until the run finishes. Progress dots go to stderr.                                            |
| `--mode`         | Update runs only: `security` or `all`. Without it, the run uses the group's setting.                                |
| `--batch`        | Update and reboot runs: servers at the same time, 1 to 50. Without it, the run uses the group's setting.            |
| `--max-failures` | Update and reboot runs: stop after this many servers fail, 1 to 1000. Without it, the run uses the group's setting. |

Give exactly one of `--group` or `--server`. Every server in one run must belong to one organization.

```sh
esa runs start measure --group production
esa runs start clean --server media-01 --wait
esa runs start clean --server web-01,web-02
esa runs start update --group production --mode security
esa runs start update --group production --batch 10 --max-failures 3
esa runs start reboot --server 42 --wait
```

- `measure` reports how much a cleanup would free. It changes nothing.
- `clean` prunes idle Docker build cache, removes old VS Code server copies, and vacuums the journal, as the group's settings allow. A run on named servers leaves the journal alone.
- `update` installs package updates in batches and stops once the failure limit is reached.
- `reboot` reboots and waits for the server to come back, in batches the same way.

Without `--wait`, the command prints the run id and exits. With `--json` and `--wait`, it prints the run and exits 0 whatever the status. Without `--json`, `--wait` exits 1 when the status is not `succeeded`.

`--mode`, `--batch`, and `--max-failures` change only what they set. With `--group`, the CLI starts from the group's settings, so held packages and the reboot setting stay. With `--server`, the rest are the defaults: update mode `security`, no held packages, no reboot, batch size 1, failure limit 1.

### esa alerts

Open alerts: severity, kind, message, and when each one fired. Prints `No open alerts.` when there are none.

| Flag    | Meaning                  |
| ------- | ------------------------ |
| `--all` | Include resolved alerts. |

```sh
esa alerts
esa alerts --all --json
```

### esa completion

Prints a completion script for bash, zsh, fish, or PowerShell.

```sh
esa completion zsh
```

### esa serve and esa healthcheck

`esa serve` runs the control plane. It takes no flags and reads the environment. See [Self-hosting](/self-hosting).

`esa healthcheck` is for the container health check. It calls `/healthz` on `ESA_LISTEN` and exits 0 on HTTP 200.

## Names and exit codes

Names match without case. They are unique inside one space only. A shared group can have the same name as one of yours, so use the numeric id when names repeat. `esa servers list` and `esa groups list` show the ids.

| Code | When                                                                    |
| ---- | ----------------------------------------------------------------------- |
| 0    | The command succeeded.                                                  |
| 1    | Bad arguments, not logged in, an API error, a network error, or Ctrl-C. |

Errors go to stderr as `esa: <message>`.

Organizations, teams, and sharing do not have CLI commands yet. Use the web app or the [API](/api).
