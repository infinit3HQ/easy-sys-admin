---
title: API reference
description: HTTP API of the control plane, generated from packages/api/openapi.yaml.
---

The web app, the `esa` CLI, and the MCP endpoint all call this API. The base path is `/api/v1`. The contract file in the source tree is `packages/api/openapi.yaml`. This page is generated from that file. Do not edit it by hand. Run `pnpm --filter @esa/web generate:docs` after the contract changes.

A browser sends the session cookie set by `/auth/callback`. The CLI and other clients send `Authorization: Bearer esa_...` with a token from Settings, API tokens. A token can do at most what its user can do, and only what its scopes allow (`read`, `logs:read`, `actions`, `admin`).

Every error has the shape `{"error": {"code": "...", "message": "..."}}`. Codes include `bad_request` (400), `unauthorized` (401), `forbidden` (403), `not_found` (404), `conflict` (409), and `unavailable` (502).

Paths marked Public do not need a session or a token.

## session

| Method | Path | Auth | Summary |
| --- | --- | --- | --- |
| GET | `/auth/config` | Public | How users sign in to this install |
| GET | `/me` | Session or token | The signed-in user and their current account |
| PUT | `/me/account` | Session or token | Change the account this browser session works in |

## fleet

| Method | Path | Auth | Summary |
| --- | --- | --- | --- |
| GET | `/fleet/overview` | Session or token | Every server in the current account with its latest status, worst first |
| GET | `/keys` | Session or token | SSH key fingerprints seen in standard authorized_keys files |
| PUT | `/keys` | Session or token | Name a fingerprint in this space, or clear the name |
| POST | `/servers/{serverId}/key-baseline` | Session or token | Approve the keys currently recorded on this server as a new baseline version. Refused when the recorded keys are not current. |
| GET | `/servers/{serverId}/key-baseline/versions` | Session or token | Earlier key approvals of this server, newest first |
| GET | `/identities` | Session or token | Owners of keys in this space, with how many grants carry their keys |
| PUT | `/identities` | Session or token | Record what an owner is and their status. The name is the key label. This does not change a server. |
| GET | `/offboarding` | Session or token | Keys with this name from the files a current helper has read |

## servers

| Method | Path | Auth | Summary |
| --- | --- | --- | --- |
| POST | `/servers/probe` | Session or token | Read a server's SSH host key without logging in |
| POST | `/servers` | Session or token | Add a server with a one-time sudo login (installs esa-helper) |
| POST | `/servers/enrollments` | Session or token | Register a server and get a one-time enroll command to paste on it |
| GET | `/servers/{serverId}` | Session or token | One server with its latest snapshot and pending updates |
| PATCH | `/servers/{serverId}` | Session or token | Rename a server, move it to another group, or change its protection settings |
| DELETE | `/servers/{serverId}` | Session or token | Remove a server from esa |
| POST | `/servers/{serverId}/enrollment` | Session or token | Issue a new enroll command for a pending server |
| POST | `/servers/{serverId}/host-key` | Session or token | Accept the new host key a server presented |
| POST | `/servers/{serverId}/helper` | Session or token | Reinstall or update esa-helper with a sudo login (or the stored one) |
| GET | `/servers/{serverId}/metrics` | Session or token | Chart points for CPU, memory, disk, and load |

## server live

Live data read from a server through esa-helper.

| Method | Path | Auth | Summary |
| --- | --- | --- | --- |
| POST | `/servers/{serverId}/updates/check` | Session or token | Refresh package lists on the server (apt-get update) and list pending updates |
| GET | `/servers/{serverId}/services` | Session or token | systemd services on the server, failed first |
| POST | `/servers/{serverId}/services/{unit}/{action}` | Session or token | Start, stop, or restart a service. Protected units cannot be stopped. |
| GET | `/servers/{serverId}/containers` | Session or token | Docker containers on the server |
| GET | `/servers/{serverId}/containers/image-check` | Session or token | Compare each container's image with its registry |
| POST | `/servers/{serverId}/containers/{name}/{action}` | Session or token | Start, stop, restart, or update (pull and recreate) one container |
| GET | `/servers/{serverId}/logs` | Session or token | Journal lines of a unit, or log lines of a container (needs logs:read) |
| GET | `/servers/{serverId}/access` | Session or token | Login users, password state, sudo rules, SSH settings, recent logins |
| GET | `/servers/{serverId}/security` | Session or token | Listening ports, firewall rules, CrowdSec and fail2ban, unattended-upgrades |
| GET | `/servers/{serverId}/jobs` | Session or token | Cron entries and systemd timers |
| GET | `/servers/{serverId}/recovery` | Session or token | Backup reports an operator wrote on the server |
| GET | `/servers/{serverId}/advisories` | Session or token | Exact affected versions from an imported local feed |

## groups

| Method | Path | Auth | Summary |
| --- | --- | --- | --- |
| GET | `/groups` | Session or token | Groups in the current account |
| POST | `/groups` | Session or token | Create a group |
| GET | `/groups/{groupId}` | Session or token | One group with its last runs |
| PUT | `/groups/{groupId}` | Session or token | Replace a group's settings |
| DELETE | `/groups/{groupId}` | Session or token | Delete a group; its servers stay, without a group |

## runs

| Method | Path | Auth | Summary |
| --- | --- | --- | --- |
| GET | `/runs` | Session or token | Maintenance runs, newest first |
| POST | `/runs` | Session or token | Start a measure, clean, update, or reboot run on a group or on servers |
| GET | `/runs/{runId}` | Session or token | One run with each server's result |
| GET | `/plans` | Session or token | Typed maintenance plans waiting for a person |
| POST | `/plans` | Session or token | Store a typed plan. This does not change a server. |
| GET | `/plans/{planId}` | Session or token | One typed plan |
| POST | `/plans/{planId}/approve` | Session or token | Approve a plan from a browser session. A token cannot approve. |
| POST | `/plans/{planId}/reject` | Session or token | Reject a plan from a browser session. A token cannot reject. |
| POST | `/plans/{planId}/execute` | Session or token | Run an approved plan once. A token that can read logs cannot execute. |
| POST | `/runs/{runId}/gate` | Session or token | Continue or stop a run waiting after its canaries. Only a person in the web app can decide. |
| GET | `/runs/{runId}/report` | Session or token | Package result and checks for one update run |

## alerts

| Method | Path | Auth | Summary |
| --- | --- | --- | --- |
| GET | `/maintenance` | Session or token | Windows that quiet ordinary notifications |
| POST | `/maintenance` | Session or token | Quiet ordinary notifications for at most 7 days |
| DELETE | `/maintenance/{windowId}` | Session or token | Remove a maintenance window |
| GET | `/incidents` | Session or token | Security notices that still need a person to look at them |
| GET | `/incidents/{incidentId}` | Session or token | One security notice with its history |
| POST | `/incidents/{incidentId}/accept` | Session or token | Accept the risk until a time within 90 days, with a reason. The notice stays and returns at the expiry or when it changes. |
| POST | `/incidents/{incidentId}/snooze` | Session or token | Hide a notice until a time within 7 days. A new occurrence ends the snooze. A host key change cannot be snoozed. |
| POST | `/incidents/{incidentId}/acknowledge` | Session or token | Record that a person has reviewed this notice, with an optional note. This does not resolve it or change the server. |
| GET | `/alerts/routing` | Session or token | How this space's warnings reach its channels |
| PUT | `/alerts/routing` | Session or token | Send warnings at once or in one daily summary. Critical notices, host key changes, and new SSH keys are always sent at once. |
| GET | `/deliveries` | Session or token | The 100 most recent notification sends of this space |
| POST | `/deliveries/{deliveryId}/retry` | Session or token | Queue a send that gave up again. A send that may have arrived is not retried. |
| GET | `/alerts` | Session or token | Open alerts, or recent alerts including resolved ones |
| GET | `/alerts/rules` | Session or token | Alert rules of the account |
| PUT | `/alerts/rules/{kind}` | Session or token | Change one alert rule |
| GET | `/channels` | Session or token | Notification channels (secrets are never returned) |
| POST | `/channels` | Session or token | Add a notification channel (owner only) |
| PATCH | `/channels/{channelId}` | Session or token | Turn a channel on or off |
| DELETE | `/channels/{channelId}` | Session or token | Delete a channel |
| POST | `/channels/{channelId}/test` | Session or token | Send a test notification |
| GET | `/certs` | Session or token | TLS certificates checked daily, soonest expiry first |
| POST | `/certs` | Session or token | Add a hostname to check |
| DELETE | `/certs/{certId}` | Session or token | Stop checking a hostname |

## settings

| Method | Path | Auth | Summary |
| --- | --- | --- | --- |
| GET | `/tokens` | Session or token | Your API tokens in this account (browser sessions only) |
| POST | `/tokens` | Session or token | Create an API token for the CLI or an MCP client. The secret is shown once. |
| DELETE | `/tokens/{tokenId}` | Session or token | Revoke a token |
| GET | `/activity` | Session or token | Audit log of the account, newest first (admins) |
| GET | `/settings/ssh-key` | Session or token | The public key the control plane logs in to servers with |

## organizations

Every user has one personal space, private to them. An organization is a space with several members, each an owner, admin, or viewer. Inside an organization, teams give their members admin rights on chosen server groups. The API calls a space an account; the current account is the one the session works in (see switchAccount).

| Method | Path | Auth | Summary |
| --- | --- | --- | --- |
| POST | `/orgs` | Session or token | Create an organization; the caller becomes its owner (browser sessions only) |
| GET | `/org` | Session or token | The current account |
| PATCH | `/org` | Session or token | Rename the current account or its two built-in teams (owners and admins) |
| DELETE | `/org` | Session or token | Delete the current organization with its servers, groups and history (owners only; not the personal space) |
| GET | `/org/members` | Session or token | Members of the current account |
| PATCH | `/org/members/{userId}` | Session or token | Change a member's role. Owners and admins move people between admin and viewer; only owners grant or take away owner. An organization always keeps at least one owner. |
| DELETE | `/org/members/{userId}` | Session or token | Remove a member (owners and admins; only owners remove owners), or leave the organization (your own user id). The last owner cannot leave. |
| GET | `/org/teams` | Session or token | Teams of the current organization. The two built-in teams (admins and viewers) are the organization roles and are not listed here. |
| POST | `/org/teams` | Session or token | Create a team that gives its members admin rights on chosen server groups (owners and admins) |
| PUT | `/org/teams/{teamId}` | Session or token | Rename a team and replace its server groups (owners and admins) |
| DELETE | `/org/teams/{teamId}` | Session or token | Delete a team. Its members keep their organization role (owners and admins) |
| PUT | `/org/teams/{teamId}/members/{userId}` | Session or token | Add a member of the organization to a team (owners and admins) |
| DELETE | `/org/teams/{teamId}/members/{userId}` | Session or token | Take a member out of a team (owners and admins) |
| GET | `/org/invites` | Session or token | Pending invites of the current organization (owners and admins) |
| POST | `/org/invites` | Session or token | Create a one-time invite link (owners and admins; only owners invite owners). The link is shown once. |
| DELETE | `/org/invites/{inviteId}` | Session or token | Revoke a pending invite |
| GET | `/invites/{token}` | Session or token | What an invite link offers, for the signed-in user |
| POST | `/invites/{token}/accept` | Session or token | Join the organization and switch this session to it (browser sessions only) |

## sharing

An organization can share a group, and the servers in it, with another organization. The owner keeps the SSH key, the group's settings and schedules, and alerts. "view" lets the other organization see the servers; "operate" also lets its admins start runs, act on services and containers, and read logs. Runs they start are recorded in the owning organization.

| Method | Path | Auth | Summary |
| --- | --- | --- | --- |
| GET | `/groups/{groupId}/shares` | Session or token | Teams this group is shared with (admins of the owning team) |
| GET | `/groups/{groupId}/share-links` | Session or token | Pending share links of a group (admins of the owning team) |
| POST | `/groups/{groupId}/share-links` | Session or token | Create a one-time link that shares the group with another team. The link is shown once. |
| DELETE | `/share-links/{linkId}` | Session or token | Revoke a pending share link |
| PATCH | `/group-shares/{shareId}` | Session or token | Change a share's access (admins of the owning team) |
| DELETE | `/group-shares/{shareId}` | Session or token | Stop sharing (admins of the owning team), or remove a group shared with you (admins of the receiving team) |
| GET | `/shares/{token}` | Session or token | What a share link offers, and which of the caller's teams can accept it |
| POST | `/shares/{token}/accept` | Session or token | Add the shared group to one of your teams (you must be an owner or admin there; browser sessions only) |

