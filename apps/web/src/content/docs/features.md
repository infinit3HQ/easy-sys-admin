---
title: Features
description: Status, updates, cleanups, schedules, services, containers, logs, reports, alerts, spaces, tokens, and the audit log.
---

Everything below is in the hosted beta. The CLI does not yet cover organizations, teams, or sharing. Those are in the web app and the API.

## Status

Each ready server is polled about once a minute. The snapshot includes CPU, memory, load, disks and inodes, network, time sync, whether a reboot is required, failed systemd units, and a Docker summary.

Samples are kept 48 hours by default. Hourly averages are kept 90 days. Charts for a short window use the raw samples. Longer windows use the hourly averages. A server that stops answering is polled every 5 minutes instead of every minute. The TCP connect gives up after 5 seconds.

The board loads the servers in the current space every 15 seconds. A server whose helper is too old to collect the exposure summary is counted at the top of the board.

Each server can carry a short note. It stays in the control plane and is shown on the board. It is not sent to the machine.

On a server's status tab, the fullest disk can show about how many days it has until it fills. The estimate uses up to 14 days of samples. A flat or shrinking disk shows no estimate.

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

The Keys page lists each fingerprint across the servers you can see, and lets an admin give it a name. The list is only the standard `authorized_keys` files of accounts with a login shell. A key in another file, or one installed by a certificate, is not included. Naming a key does not change the server. Looking up a name shows where that name still appears in the files a current helper has read. A missing row is not proof that access was removed. An admin can approve the keys currently recorded on one server. Later checks report a key that appears, disappears, or changes its restriction. The approval does not change the server. A helper older than the current report is marked, and its last complete summary is kept instead of being replaced with an empty one.

The access report lists users with a login shell, whether each password is locked, empty, or set, and the fingerprints of their SSH keys. It does not return the password hash or the key. It lists sudo rules from `/etc/sudoers` and `/etc/sudoers.d`, including `NOPASSWD` and `ALL`. Aliases are not expanded, and includes that point outside that directory are skipped. It also shows who is in the sudo and docker groups, recent logins, failed SSH attempts, and the SSH server's password, root, empty-password, public-key, and forwarding settings.

The security report lists listening ports, the user that owns each socket, and whether the socket is on every interface, on loopback, or on one address. A `docker-proxy` socket is joined to the container that publishes it. A port Docker publishes is listed even when `ss` does not show it, and its firewall result is unknown, because Docker can bypass ufw. The report shows ufw rules when ufw is active, up to 40 lines. nftables is named only. It also shows unattended-upgrades. CrowdSec and fail2ban are shown only when that server has them installed. The same page shows restic or Borg result files from `/var/lib/esa/recovery` when an operator put them there. Esa does not run those tools. A success in the file is not proof the data can be restored.

The board keeps a short summary of that picture, refreshed about once an hour: sockets listening on all interfaces, sockets on one address, those SSH flags, the firewall name, extra uid 0 accounts, and `NOPASSWD ALL` rules. A socket on every interface is not proof that a network can reach it. A socket on one address is not proof either way. If a check does not run, the summary names that gap instead of treating the missing list as a clean result. The summary shows when it was last collected and when the control plane last tried. The user list and login addresses are not stored. A helper older than this summary says so, and its empty fields are not treated as a clean result. Reinstall the helper to collect it.

An exposure alert is off until you turn it on under alert settings. It fires for password login, empty passwords, root password login, no firewall, an extra uid 0 account, or a `NOPASSWD ALL` rule. Host key changes, weak exposure, and old security updates also appear in the review list. A host key change cannot be snoozed.

A maintenance window, set under alert settings, quiets ordinary notices for at most 7 days. Checks still run. A host key change and a new SSH key are still sent.

After a package upgrade, the control plane reads status again. A failed unit that was not already failed, or more unhealthy or restarting containers than before, fails that server. A problem that was already there does not. A batch can then stop.

A group can store up to eight checks: a TCP connect, an HTTP GET or HEAD, a systemd service, or a container. Each one uses the server's enrolled address. You cannot type a different host. HTTP does not follow a redirect. A blocking failure stops that server, before apt and again after. An advisory failure is written on the run and the upgrade continues. When you start an update you can mark canaries. Those servers go first. If one of them fails, the others are skipped. You can watch the canaries for up to 10 minutes. The checks repeat every 15 seconds, and a failure skips the rest. The run page and `GET /runs/{id}/report` list the packages, the checks, and why a server was skipped.

An agent can propose a typed plan. The Plans page is where a person approves or rejects it. A token cannot approve. Approval lasts 30 minutes. Running it twice returns the same run. A token that can read logs cannot run a plan.

Two more alerts are on by default. A new socket on all interfaces, and a new SSH key, each send one notice. The socket notice does not claim the internet can connect. The first report seeds the baseline and does not alert. A security update that is still pending after 7 days also alerts. The wait is the number of days in alert settings. The board already turns yellow as soon as a security update is pending.

When ufw is active, each listening socket says whether ufw allows it, denies it, or does not mention it. Sockets on every interface that ufw does not mention are counted on the board. If fail2ban is installed, each jail shows how many addresses it currently bans. If CrowdSec is installed, the report shows how many decisions are active. The addresses are not collected. A server without either tool is not marked for that. An SSH key shows its `from=` restriction, and whether it forces a command. The command text is not shown. A uid 0 key with neither restriction is marked.

Scheduled jobs lists cron entries and systemd timers. Cron command lines are hidden from a token that cannot read logs, because those lines often contain secrets.

A group with two or more servers that have a current exposure summary lists where they differ: password login, root login, firewall, and sockets listening on all interfaces. A socket that is not on every server in the group is named. Reachability is not verified.

## Alerts

An alert fires after its condition has held for the duration you set, and it resolves when the condition clears. Kinds include disk, inodes, memory, unreachable, failed unit, unhealthy container, certificate expiry, failed run, host key change, exposure, a new socket on all interfaces, a new SSH key, and security updates that have been waiting. Exposure is off until you enable it. The new-port and new-key notices fire once and stay on the alerts page until an admin marks them reviewed or snoozes them for at most 7 days. A snooze comes back on its own. A later event for the same thing opens it again. Reviewing or snoozing does not change the server. Notifications are stored before they are sent, and a restart retries them. Five failures leave the delivery marked dead. A maintenance window quiets ordinary notices and does not hide a host key change or a new SSH key. A security update alerts after the number of days you set, 7 by default.

Channels are ntfy, a generic webhook, Discord, Slack, and email. You can send a test message. Certificate checks are a list of TLS endpoints the control plane connects to.

On Monday after 08:00 UTC, each space with a channel gets one summary if there is something to say: unreachable servers, security updates that have been waiting at least 7 days, and new sockets on all interfaces or SSH keys from the past week. A quiet week sends nothing. If the control plane is down that Monday, that week is skipped.

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
