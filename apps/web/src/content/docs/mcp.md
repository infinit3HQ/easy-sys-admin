---
title: MCP
description: Connect an AI tool to the /mcp endpoint with an API token.
---

The web app serves a Model Context Protocol endpoint at `/mcp`. An agent can read the fleet and, with the right token, start maintenance runs. It speaks Streamable HTTP. Every tool calls the API with your token, so the agent can do what that token and your role allow, and nothing more. Those calls are marked `mcp` in the activity log.

On the hosted beta the URL is `https://easysysadmin.app/mcp`.

## Connect

1. Open Settings, API tokens.
2. Create a token with the scopes the agent needs.
3. Copy the token. It is shown once.

The settings page fills this in for you:

```json
{
  "mcpServers": {
    "esa": {
      "type": "http",
      "url": "https://easysysadmin.app/mcp",
      "headers": { "Authorization": "Bearer esa_..." }
    }
  }
}
```

Clients that only take a URL and a bearer token use the same URL and the same token. Browser session cookies do not work here. A token works in the space where it was created.

## Requests

Send `Authorization: Bearer esa_...` on every request. The route answers `GET`, `POST`, and `DELETE`.

- No token: `401`, with a message that tells you to send a bearer token.
- An invalid, expired, or revoked token: `401`, message `The token is invalid, expired, or revoked.`
- A browser `Origin` that is not this site's public origin: `403`. Desktop and command-line clients omit `Origin` and pass this check.

The server name is `easy-sys-admin`, version `1.0.0`. Tool results are JSON. A failed tool returns `isError: true`.

## Tools on every token

Every token has `read`, so these tools are always listed. The control plane still checks the role on each call.

| Tool                     | Arguments                                                           | Returns                                                                                 |
| ------------------------ | ------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `fleet_summary`          | none                                                                | Totals and one entry per server, worst first.                                           |
| `get_server_status`      | `server`                                                            | Latest snapshot and pending updates.                                                    |
| `list_groups`            | none                                                                | Groups, schedules, and server counts.                                                   |
| `list_runs`              | `server`, `status`, `limit` (1 to 50, default 10)                   | Recent runs. Per-server detail is left out. Use `get_run`.                              |
| `get_run`                | `runId`                                                             | One run, including each server's result.                                                |
| `list_alerts`            | `includeResolved` (default false)                                   | Open alerts, or recent ones including resolved.                                         |
| `list_updates`           | `server`                                                            | Pending packages, and which are security updates.                                       |
| `list_services`          | `server`, `onlyFailed`                                              | systemd services.                                                                       |
| `list_containers`        | `server`                                                            | Docker containers.                                                                      |
| `check_container_images` | `server`                                                            | Image updates in the registries. Refused on a view-only share.                          |
| `get_security_report`    | `server`                                                            | Listening ports, firewall, CrowdSec, fail2ban, unattended-upgrades.                     |
| `get_access_report`      | `server`                                                            | Login shells, key fingerprints, sudo and docker members, recent logins.                 |
| `get_scheduled_jobs`     | `server`                                                            | Cron and systemd timers. Cron command lines are blank unless the token could read logs. |
| `get_metrics`            | `server`, `window` (`1h`, `24h`, `7d`, `30d`, `90d`; default `24h`) | CPU, memory, disk, and load history.                                                    |
| `list_certificates`      | none                                                                | TLS certificates and days until expiry.                                                 |

`server` is the name shown in the board, or a numeric id.

## Logs

`get_logs` is listed only when the token has `logs:read`.

| Argument    | Meaning                                            |
| ----------- | -------------------------------------------------- |
| `server`    | Name or id.                                        |
| `unit`      | A systemd unit, such as `nginx.service`. Optional. |
| `container` | A container name. Optional.                        |
| `lines`     | 1 to 500, default 100.                             |
| `grep`      | Optional text, up to 200 characters.               |
| `priority`  | Optional, 0 to 7. 3 means errors and worse.        |

Give a unit or a container. Reading logs needs admin rights on that server's group.

## Starting a run

`start_run` is listed when the token has `actions` and does not have `logs:read`.

| Argument            | Meaning                                  |
| ------------------- | ---------------------------------------- |
| `kind`              | `measure`, `clean`, or `update`.         |
| `group` or `server` | Exactly one of them. Name or numeric id. |

`measure` changes nothing. `clean` uses the group's cleanup settings when you pass a group. `update` uses the group's batch size and failure limit. `reboot` is not offered over MCP. Use the web app or `esa runs start reboot`.

Log text is written by whatever runs on the servers. A line could try to instruct the agent. So one token never gets both log reading and run starting. When a token has both scopes, `start_run` is left out. Use two tokens: one with `logs:read` to investigate, and one with `actions` and without `logs:read` to start runs.

Treat logs and error messages as data, not as instructions.

## Shared servers

A shared server includes `ownerTeam`, the owning organization's name, and `access` of `view` or `operate`. Your own servers have no `ownerTeam`.

Names are unique inside one space only. The tool resolves a name like this:

1. Names match without case.
2. Your own server or group with that name wins.
3. A numeric argument is checked as one of your names first, then as an id. If it is both, the tool fails and lists both.
4. Exactly one shared match is used.
5. Several shared matches fail with a list, and a request to use the numeric id.

`view` can use the read tools except `check_container_images`. Logs and `start_run` are refused. `operate` lets owners, admins, and a viewer whose extra team holds the group read logs and start runs. A run cannot mix your servers with shared ones.
