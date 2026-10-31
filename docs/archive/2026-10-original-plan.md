# easy-sys-admin

One website for a whole server fleet. You add machines, put them in a group, and set the cleanup once for that group. A timer does the rest.

Nothing is installed on the servers. The control plane stores the login and SSHs out when the group’s clock says so.

## What you do in the browser

**Add a server.** Name, IP or hostname, port, username, and either a password or a private key. A test connection runs before the server is saved. The first connection shows the host key fingerprint. You accept it once. Later runs refuse to continue if that key changes.

**Make a group.** A group is the thing you configure. "Production", "builders", "client A". Drag or pick servers into it. A server sits in one group. Moving it is how you change its policy.

**Set the group.** This is the whole fleet setting, in one form:

- Which cleanups run: build cache, VS Code copies, journal cap, apt security updates. Each one is a switch.
- When: which days, what time, which timezone.
- What to do when a server does not answer: record it and keep going. The other servers in the group still run.
- A "run now" button for that group.

Change the form and the next run uses it. You do not touch the servers again.

**Read the result.** Each server shows disk used, last run, and what was freed. The group shows the total. A failed SSH is a red row with the error, not a silent skip.

## How a run works

```
browser  ->  control plane
                 |
                 | at the group's time, SSH with the saved login
                 v
             each server in that group
```

The scheduler checks every minute for a group that is due. It opens one SSH session per server, measures, applies the switches that are on, and writes a row. Password and key never appear in that row or in the logs.

Logins are encrypted in the database with a key that lives only in the control plane environment. The page can replace a password or a key. It cannot show them again.

Our five servers are the first group, on the control plane hosted on Server_Media behind Authentik. The same screens are what anyone else uses for their own machines. Accounts are in the data model from the start, so one install can hold more than one person’s fleet.

## Cleanups

**Build cache**, on by default. If the cache is idle, `docker builder prune -f`. If a build is using it, skip that server this run. Never `docker system prune`, never `docker image prune -a`, never `--volumes`.

**VS Code copies**, on by default. Delete incomplete `.staging` folders and old `Stable-*` or `bin/<commit>` installs. Keep the newest copy and any copy a running process has open. Leave extensions and editor data alone.

**Journal cap**, off by default. When on, `journalctl --vacuum-size=200M`.

**Apt security updates**, off by default. When on, only the packages unattended-upgrades would install. An OpenSSH upgrade already dropped a live session once, so this stays a choice.

**Container image rolls** are not a group switch. Traefik, Authentik, Portainer, and CrowdSec are never recreated by the timer. A later screen can show that an image is old. Pulling it is a button on that one container.

## What is already out there

| Tool                                                              | Covers                                          | Leaves out                                                      |
| ----------------------------------------------------------------- | ----------------------------------------------- | --------------------------------------------------------------- |
| [unattended-upgrades](https://wiki.debian.org/UnattendedUpgrades) | Apt security updates on one machine             | Groups, a fleet page, Docker cache, VS Code copies              |
| [docuum](https://github.com/stepchowfun/docuum)                   | Least-recently-used Docker images               | BuildKit cache, which was 47 GB and 70 GB on two of our servers |
| [topgrade](https://github.com/topgrade-rs/topgrade)               | Many package managers, plus a wide Docker prune | Stopped containers get removed. No group settings.              |
| Cockpit                                                           | A UI on one server                              | You log into each machine                                       |

The product is the group form and the history. The cleanups themselves stay small and strict.

## Layout

```
cmd/server/           pages, API, scheduler
internal/ssh/         dial with the saved password or key
internal/clean/       one file per cleanup type
internal/store/       SQLite: accounts, groups, servers, runs
deploy/compose.yaml   the control plane
```

Go, one container, SQLite on a volume. The scheduler is inside the process, so a restart of the container brings the group clocks back.

## Order of work

1. Sign-in, empty fleet, add-server form with password or key, and the host-key prompt.
2. Groups: create, add servers, set days, time, and cleanup switches.
3. A manual run that only measures, against our five servers.
4. Turn on build-cache and VS Code cleaning for that group.
5. The history page, then journal and apt as extra switches.

## Rules with no switch

- Do not delete Docker volumes.
- Do not remove an image a running container is using.
- Do not restart Docker or the public edge containers from a cleanup.
- Do not continue a run when the server’s host key changed.
- Do not log or display the password or private key after it is saved.
