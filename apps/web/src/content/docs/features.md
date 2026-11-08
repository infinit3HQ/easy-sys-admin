---
title: Features
description: Status, updates, cleanups, schedules, services, containers, logs, reports, alerts, spaces, tokens, and the audit log.
---

Everything below is in the hosted beta. The CLI does not yet cover organizations, teams, or sharing. Those are in the web app and the API.

## Status

Each ready server is polled about once a minute. The snapshot includes CPU, memory, load, disks and inodes, network, time sync, whether a reboot is required, failed systemd units, and a Docker summary.

Samples are kept 48 hours by default. Hourly averages are kept 90 days. Charts for a short window use the raw samples. Longer windows use the hourly averages. A server that stops answering is polled every 5 minutes instead of every minute. The TCP connect gives up after 5 seconds.

The board loads the servers in the current space every 15 seconds.

## Updates

The pending list splits security updates from the rest. Held packages are shown.

An update run installs security updates only, or all pending updates, in batches. The group's batch size is how many servers are upgraded at the same time. The failure limit stops the run after that many servers fail. The rest are skipped. The default is one server at a time, and stop at the first failure. The server that hosts the control plane is always in a batch of its own.

The upgrade runs in a transient systemd unit, so closing the SSH session does not kill `apt`. Existing config files are kept. `needrestart` is told to list services instead of restarting them. After the upgrade, the run can reboot if the group asks for that and the server says a reboot is required. A scheduled reboot skips the control plane's own server. A reboot you start yourself does not use that skip.

The control plane waits for a new boot time, then checks that containers with a restart policy came back.

Checking the list (`updates --refresh`) runs `apt-get update` and does not install anything.

## Cleanups

A cleanup run can do three things:

| Cleanup     | What it removes                                                                                                                    |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| Build cache | Idle Docker build cache. Skipped while a build is running. Not a full cache prune.                                                 |
| VS Code     | Old copies under `.vscode-server` in `/root` and in `/home/*`. The newest copy, and any copy a running process is using, are kept. |
| Journal     | `journalctl --vacuum-size`. The size looks like `200M` or `1G` and must be at least 50M. The default is `200M`.                    |

A measure run reports how much space those cleanups would free and changes nothing. A cleanup started on named servers, rather than on a group, cleans build cache and VS Code copies and leaves the journal alone.

## Schedules

Each group has a cleanup schedule and an update schedule, in a timezone you set. A slot runs once. A slot missed by more than an hour is recorded as missed. A schedule that is off does not run.

## Services

The services tab lists loaded systemd services, failed ones first. You can start, stop, or restart one `.service` unit. Protected units such as `ssh`, `sshd`, and Docker cannot be stopped. Network and firewall units cannot be acted on. Power, reboot, and rescue units cannot be started from this list.

## Containers

The containers tab shows state, health, restart count, and compose labels. You can start, stop, or restart one container. An image check compares the local image with the registry.

Pulling and recreating is one compose service at a time. Containers whose names start with `traefik`, `authentik`, `portainer`, or `crowdsec`, or a prefix you set on that server, need you to confirm the exact name.

## Logs

Journal lines for one unit, or the log of one container. The API allows 1 to 1000 lines. The default is 200. A text filter is cut at 200 characters. Reading logs needs the `logs:read` scope on a token, and admin rights on that server's group.

## Access and security reports

The access report lists users with a login shell, the fingerprints of their SSH keys, who is in the sudo and docker groups, recent logins, and failed SSH attempts.

The security report lists listening ports and their processes, the firewall, CrowdSec, fail2ban, and unattended-upgrades.

Scheduled jobs lists cron entries and systemd timers. Cron command lines are hidden from a token that cannot read logs, because those lines often contain secrets.

## Alerts

An alert fires after its condition has held for the duration you set, and it resolves when the condition clears. Kinds include disk, inodes, memory, unreachable, failed unit, unhealthy container, certificate expiry, failed run, and host key change.

Channels are ntfy, a generic webhook, Discord, Slack, and email. You can send a test message. Certificate checks are a list of TLS endpoints the control plane connects to.

## Organizations and spaces

|                            | Personal space    | Organization                               |
| -------------------------- | ----------------- | ------------------------------------------ |
| Who is in it               | Only you          | Members you invite                         |
| Roles                      | You are the owner | Owner, admin, viewer                       |
| Can be deleted             | No                | Yes, by an owner, after they type the name |
| Can receive a shared group | No                | Yes                                        |

On the hosted beta you can own one organization besides the personal space. A self-hosted install with the beta caps turned off allows 20 organizations you own, unless that setting is changed.

A viewer can read the organization. An admin manages members, teams, servers, groups, runs, logs, and alerts. An owner can also rename or delete the organization and appoint another owner. There is always at least one owner.

Extra teams grant admin on the groups you pick. A member of an extra team can run those groups and read their logs. They still cannot add or delete servers, share groups, or change alerts for the whole organization.

An invite is a one-time link, valid 7 days. A share link is the same idea for handing a group to another organization, as view or operate. The owning space keeps the SSH key, the schedules, and the alerts.

## API tokens and scopes

Create a token under Settings, API tokens. Copy it then. It is stored as a hash and cannot be shown again. Choose an expiry of 30, 90, or 365 days. The API default, when a client does not send one, is 90 days. Every token expires. The allowed range is 1 to 365 days.

| Scope       | Allows                                                      | Who can grant it                                        |
| ----------- | ----------------------------------------------------------- | ------------------------------------------------------- |
| `read`      | Board, servers, groups, runs, alerts. Always included.      | A viewer                                                |
| `logs:read` | Server and container logs                                   | An admin, or a viewer who is in at least one extra team |
| `actions`   | Runs, service and container actions, reboots, update checks | An admin, or a viewer who is in at least one extra team |
| `admin`     | Servers, groups, alert rules, channels, certificates        | An admin                                                |

A token works in the space where it was created. Demoting or removing a member limits or disables their tokens at once. A token cannot create or revoke tokens, and cannot create or accept invites or share links.

## Audit log

The activity page is the audit log. Each change is recorded with the actor and a short detail. Calls are marked web, API, MCP, or system. The log is not pruned. It does not contain passwords, private keys, or token secrets.
