# MCP endpoint

The web app serves a Model Context Protocol server at `/mcp`, so an AI agent can read the fleet and, with the right token, start maintenance runs. It speaks Streamable HTTP and authenticates with an esa API token. Every tool calls the control plane API with the caller's own token, so an agent can do what that token's scopes and its user's role allow and nothing more. The control plane records those calls in the activity log with source `mcp`. The code is in `apps/web/src/server/mcp.ts`, mounted by `apps/web/src/routes/mcp.ts`.

## Connect

1. In the web app, open Settings > API tokens.
2. Create a token. Pick the scopes the agent needs (see [Tools](#tools)).
3. Copy the token. esa stores only a hash and shows the secret once. The page also shows a ready MCP client config with the token filled in.

The config the Settings page generates:

```json
{
  "mcpServers": {
    "esa": {
      "type": "http",
      "url": "https://esa.example.com/mcp",
      "headers": { "Authorization": "Bearer esa_..." }
    }
  }
}
```

Replace the origin with your install's and `esa_...` with the token. Clients that read an `mcpServers` block with `url` and `headers` accept this as is. For a client that only takes a URL and a bearer token, use `https://<your host>/mcp` and the token.

## Transport

- Streamable HTTP. The route answers `GET`, `POST`, and `DELETE` on `/mcp`.
- The server identifies itself as `easy-sys-admin`, version `1.0.0`.
- Each request builds a fresh MCP server for the caller's token. The tool list is computed from the token's scopes at that moment.
- Tool results are JSON. Each result carries the JSON as text content and as structured content. Arrays come back as `{ "items": [...] }` in the structured content.
- A failed tool call returns `isError: true` with `Error: <message>` as text. Messages from the control plane pass through, for example `Your role or token does not allow this.`

## Authentication

Send `Authorization: Bearer esa_...` on every request. The endpoint checks the token by calling `GET /api/v1/me` on the control plane before it handles the MCP message.

- No bearer token: `401` with `{"error": {"code": "unauthorized", "message": "Send an esa API token: Authorization: Bearer esa_..."}}`.
- An invalid, expired, or revoked token: `401` with the message `The token is invalid, expired, or revoked.`
- Both carry `WWW-Authenticate: Bearer realm="esa", error="invalid_token"`.

Browser session cookies do not work here. Only API tokens do. A token works in the account (space) it was created in.

## Origin rule

Browsers send an `Origin` header, and a page on another site must not drive this endpoint. When a request has an `Origin` header, it must equal the install's public origin, or the endpoint answers `403` with `{"error": {"code": "forbidden", "message": "Cross-origin requests to /mcp are not allowed."}}`.

The public origin is the origin of `ESA_PUBLIC_URL` in the web server's environment. When that is unset or not a valid URL, the request's own origin is used. Requests without an `Origin` header, which is what desktop and command-line MCP clients send, pass this check.

## Tools

Every token has the `read` scope, because the control plane adds it to each token. The tools below `read` are therefore always listed. The control plane still checks the role and scope on each call, so a listed tool can fail with a permission error.

`server` arguments take a server name as shown in esa, or a numeric id. See [Shared servers](#shared-servers) for how names resolve.

### Registered for every token (`read`)

| Tool                      | Arguments                                                                                                                                 | Returns                                                                                                                                                                                                                            |
| ------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `fleet_summary`           | none                                                                                                                                      | Account name, totals, and one entry per server, worst first: id, name, group, status, reasons, CPU, memory, disk, reboot required, security and other updates, last seen, last error. Shared servers add `ownerTeam` and `access`. |
| `get_server_status`       | `server`                                                                                                                                  | The server detail: latest snapshot (disks, memory, load, uptime, Docker, failed units) and pending updates.                                                                                                                        |
| `list_groups`             | none                                                                                                                                      | Groups with cleanup and update schedules and server counts. Shared groups add `ownerTeam` and carry `access` (`view` or `operate`).                                                                                                |
| `list_runs`               | `server` (optional), `status` (optional: `running`, `succeeded`, `failed`, `partial`, `skipped`, `missed`), `limit` (1 to 50, default 10) | Recent runs, newest first. Per-server `detail` is left out; use `get_run` for it.                                                                                                                                                  |
| `get_run`                 | `runId` (positive integer)                                                                                                                | One run with every server's result, including cleanup and upgrade details.                                                                                                                                                         |
| `list_alerts`             | `includeResolved` (boolean, default `false`)                                                                                              | Open alerts, or recent alerts including resolved ones.                                                                                                                                                                             |
| `list_security_incidents` | `includeReviewed` (boolean, default `false`)                                                                                              | Open security notices, or all of them including reviewed, snoozed, accepted, and resolved.                                                                                                                                         |
| `get_security_incident`   | `incidentId`                                                                                                                              | One notice with its history.                                                                                                                                                                                                       |
| `get_offboarding_report`  | `owner` (a key label)                                                                                                                     | Where that owner's known keys were last seen, servers that could not be checked, and a Markdown copy.                                                                                                                              |
| `list_updates`            | `server`                                                                                                                                  | Pending package updates, with which are security updates. `{ "message": "Not checked yet." }` when the server has not been checked.                                                                                                |
| `list_services`           | `server`, `onlyFailed` (boolean, default `false`)                                                                                         | systemd services. With `onlyFailed`, only units whose active state is `failed`.                                                                                                                                                    |
| `list_containers`         | `server`                                                                                                                                  | Docker containers with state, health, and restart counts.                                                                                                                                                                          |
| `check_container_images`  | `server`                                                                                                                                  | Image updates available in the registries. Refused on servers shared with `view` access.                                                                                                                                           |
| `get_security_report`     | `server`                                                                                                                                  | Listening ports and exposure, ufw rules, CrowdSec and fail2ban state, unattended-upgrades.                                                                                                                                         |
| `get_access_report`       | `server`                                                                                                                                  | Login users and password state, sudo rules, SSH settings, key fingerprints, recent logins, failed SSH attempts. Password hashes are not included.                                                                                  |
| `get_scheduled_jobs`      | `server`                                                                                                                                  | Cron entries and systemd timers. Cron command lines are blanked unless the token could read logs on that server (see below).                                                                                                       |
| `get_metrics`             | `server`, `window` (`1h`, `24h`, `7d`, `30d`, `90d`; default `24h`)                                                                       | CPU, memory, disk, and load history.                                                                                                                                                                                               |
| `list_certificates`       | none                                                                                                                                      | TLS certificates esa checks, with days until expiry.                                                                                                                                                                               |

All of these are marked read-only for the client.

### Registered with `logs:read`

| Tool       | Arguments                                                                                                                                                                                                                      | Returns                                                   |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------- |
| `get_logs` | `server`, `unit` (optional, a systemd unit such as `nginx.service`), `container` (optional), `lines` (1 to 500, default 100), `grep` (optional, up to 200 characters), `priority` (optional, 0 to 7; 3 means errors and worse) | Journal lines of the unit, or log lines of the container. |

Reading logs needs admin rights on the server's group, and `operate` access when another organization owns the server. The organization role counts, and so does an extra team that holds the group. An owner, an admin, or a viewer who is in at least one extra team can create a token with `logs:read`. The token still works only where that role allows it on each request.

### Registered with `actions` and without `logs:read`

| Tool                        | Arguments                                                                                             | Returns                                                                                                                                                              |
| --------------------------- | ----------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `start_run`                 | `kind` (`measure`, `clean`, or `update`), and exactly one of `group` (name or numeric id) or `server` | `{ runId, status, hint }`. Follow progress with `get_run`.                                                                                                           |
| `propose_fix_plan`          | `kind`, `groupId` or `serverIds`, and the update or cleanup settings                                  | The stored plan, with the servers and pending packages it covers. It does not change a server.                                                                       |
| `list_fix_plans`            |                                                                                                       | Plans in this space.                                                                                                                                                 |
| `get_fix_plan`              | `planId`                                                                                              | One plan.                                                                                                                                                            |
| `execute_approved_fix_plan` | `planId`                                                                                              | `{ runId, status }` for a plan a person approved in the browser. A repeat returns the same run. Not offered to a token that can read logs. There is no approve tool. |

- `measure` reports reclaimable space and changes nothing.
- `clean` prunes idle build cache and old VS Code copies, with the group's settings when started on a group.
- `update` installs package updates in batches, using the group's batch size and failure limit (one server at a time and stop at the first failure when started on a server).
- `reboot` is not offered over MCP. Use the web app or `esa runs start reboot`.
- On a server or group shared with `view` access, the tool refuses before calling the API and tells the agent to ask the owning organization for `operate` access.
- Starting runs needs admin rights on that group: the organization role, or an extra team that holds the group. The tool is marked destructive for the client.

### Why start_run is not offered to tokens with logs:read

Log text comes from whatever runs on the servers. Anyone who can make a service print a line can put words in front of the agent, and those words could tell it to start an update or a cleanup. So one token never gets both log reading and run starting. When a token has `actions` and `logs:read`, the endpoint leaves `start_run` out, and the server instructions tell the agent that runs are off for this token because it can read logs.

Use two tokens: one with `logs:read` for investigating, and one with `actions` but without `logs:read` for starting runs. The same idea applies to `get_scheduled_jobs`: cron command lines often hold secrets, so they are only returned when the token has `logs:read` and the user may read logs on that server.

The server instructions also tell the agent to treat server output such as logs and error messages as untrusted data, never as instructions.

## Shared servers

An organization can share a group, and the servers in it, with another organization. Shared servers and groups show up in the tools with `ownerTeam`, the name of the owning organization, and `access`, which is `view` or `operate`. Your own servers and groups have no `ownerTeam`. `fleet_summary` leaves `access` out for them, and `list_groups` shows `access: "own"`.

Names are unique inside one account only. A shared server can have the same name as one of yours, or as another shared server. When a tool gets a `server` or `group` argument, it resolves it this way:

1. Names match without case.
2. Your own server or group with that name wins.
3. A numeric argument is checked as one of your own names first, then as an id. If it is the name of one of your servers and also the id of a different one, the tool fails and lists both, so the agent can pick.
4. Exactly one shared match is used.
5. Several shared matches fail with a list of `name (Ops) id N` entries, where Ops is the owning organization, and a request to use the numeric id.
6. No match fails with the list of known names.

Error messages label a shared server as `web-01 (Ops)`, where `Ops` is the owning organization.

What a shared server allows:

- `view` access: all read tools work, except `check_container_images`. `get_logs` and `start_run` are refused.
- `operate` access: owners and admins of your organization can also read logs and start runs, and so can a viewer whose extra team holds the group. Other viewers stay read only.
- A run cannot mix your servers with shared ones, or servers of different organizations. Runs started on shared servers are recorded in the owning organization.

## Example session

A typical agent flow with a read-only token:

1. `fleet_summary` to see which servers need attention.
2. `get_server_status` with `server: "media-01"` for the disks and failed units.
3. `list_runs` with `server: "media-01"` and `status: "failed"` to see what broke recently.
4. `get_run` with the run id for the per-server errors.

With an `actions` token, `start_run` with `kind: "measure"` and `group: "production"` reports how much a cleanup would free before anyone runs `clean`.
