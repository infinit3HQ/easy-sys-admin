# The server helper

This document describes what easy-sys-admin puts on a managed server and what it can do there. It covers the `esa` user, the helper binary, the SSH key and sudo rule that tie them together, the two ways to enroll a server, host key pinning, every helper action and the safety rules the helper enforces, how updates and reboots run, how to upgrade the helper, and how to remove it. The code is in `apps/esa/cmd/esa-helper`, `apps/esa/internal/hostops`, and `apps/esa/internal/remote/install.go`. For how the control plane uses the helper, see [architecture.md](architecture.md). For the wider security model, see [security.md](security.md).

## What gets installed

The install script (`remote.InstallScript`) is the same for both enrollment paths. It runs as root and creates:

| Path or object                           | Owner and mode   | Purpose                                                                                                        |
| ---------------------------------------- | ---------------- | -------------------------------------------------------------------------------------------------------------- |
| User `esa`                               | system user      | The login the control plane uses. Comment `easy-sys-admin control plane`, shell `/bin/sh`, password field `*`. |
| `/var/lib/esa-home`                      | `root:root` 0755 | The `esa` home. Root owns it, so `esa` cannot write there.                                                     |
| `/var/lib/esa-home/.ssh`                 | `root:root` 0755 | Recreated on every install.                                                                                    |
| `/var/lib/esa-home/.ssh/authorized_keys` | `root:root` 0644 | One line: the account's key with a forced command.                                                             |
| `/usr/local/lib/esa/esa-helper`          | `root:root` 0755 | The helper binary.                                                                                             |
| `/etc/sudoers.d/esa`                     | `root:root` 0440 | The only sudo rule for `esa`.                                                                                  |
| `/var/lib/esa`                           | `root:root` 0750 | Helper state, for example upgrade results.                                                                     |

The password field `*` is not a valid password but, unlike `!`, does not lock key logins. If a user named `esa` already exists and does not carry the comment and home above, the install stops with an error instead of reusing it. The script needs root and an installed `sudo`. Only Linux on amd64 and arm64 is supported.

### The SSH key

Each account has one ed25519 key pair, created on first use. The private key is sealed in the database. The `authorized_keys` line is:

```
restrict,command="/usr/local/lib/esa/esa-helper ssh-dispatch" ssh-ed25519 AAAA... esa-control-plane
```

`restrict` turns off port, agent, and X11 forwarding and pty allocation. The forced command means sshd runs `esa-helper ssh-dispatch` as the `esa` user whatever command the client asked for.

### The sudo rule

`/etc/sudoers.d/esa` contains:

```
Defaults:esa !requiretty
Defaults:esa env_reset
Defaults:esa secure_path="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
esa ALL=(root) NOPASSWD: /usr/local/lib/esa/esa-helper *
```

The script writes it to `esa.new` first, checks it with `visudo -cf`, and only then renames it. sudo ignores files in `sudoers.d` whose name contains a dot, so the unchecked file is never live.

### How a command reaches the helper

The control plane sends commands of exactly one form, `sudo -n /usr/local/lib/esa/esa-helper <action> [args...]`, with every argument quoted by `remote.ShellQuote`. On the server:

1. sshd starts `esa-helper ssh-dispatch` as `esa`, with the requested command in `SSH_ORIGINAL_COMMAND`.
2. `ssh-dispatch` refuses to run as root. It parses the command with `hostops.DispatchArgs`, which accepts only words separated by single spaces, each either bare (letters, digits, and `-_./,=:@%+`) or one single-quoted string. Any other shell syntax is refused. The first three words must be `sudo`, `-n`, and the helper path, and the action cannot be `ssh-dispatch` itself.
3. It replaces itself with `/usr/bin/sudo` (or `/bin/sudo`) with an environment of only `PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`. No shell is involved.
4. The control plane sends an action only when the helper listed it in its last `version` answer. A helper older than that list is assumed to have the actions helpers had before it (`proto.LegacyActions`), so a new action is never sent to an old helper. A stored list that is not valid JSON allows no action. It is not treated as that older set. A refused action fails with code `helper_incompatible` and asks for a reinstall. The answer is asked for at enrollment, at reinstall, and then at most once a day; a server that does not answer is asked again after an hour.
5. The helper runs as root. It sets the same fixed `PATH`, refuses every action except `version` when it is not root, and prints one JSON envelope on stdout (`hostops/proto.Envelope`).

Programs the helper starts get a fixed environment (`PATH`, `HOME=/root`, `LC_ALL=C`, `LANG=C`, and the apt settings listed under [Updates](#updates)). Nothing is inherited from the caller. Docker is reached through its Engine API socket. The helper has no network listener.

## Enrolling a server

Both paths need the admin role (and the `admin` scope for an API token). An account can have at most 500 servers by default, or 10 when `ESA_BETA_LIMITS` is on.

|                    | Path A: enroll command                                                             | Path B: one-time sudo login                                   |
| ------------------ | ---------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| What you do        | Paste one command on the server                                                    | Give esa a user and password or private key that can use sudo |
| Who connects first | The server calls esa over HTTPS                                                    | esa connects to the server over SSH                           |
| Host key           | Reported by the script, then confirmed by esa over SSH                             | Read by esa first, compared and accepted by you               |
| Login stored       | Never                                                                              | Only with `keepLogin`, which only owners can set              |
| Needs              | `ESA_PUBLIC_URL` on https (or a loopback address), `curl`, `sha256sum` or `shasum` | SSH access with a sudo-capable login                          |

### Path A: the enroll command

`CreateEnrollment` in `apps/esa/internal/fleet/server_enroll.go` registers the server as `pending` and returns a command:

```
curl -fsSL <ESA_PUBLIC_URL>/enroll/<token> | sudo sh
```

The token starts with `enr_`, is valid for 15 minutes, works once, and is stored only as a SHA-256 hash. Asking for a new command for a pending server makes older commands stop working. esa refuses to issue enroll commands when `ESA_PUBLIC_URL` is plain `http` and not a loopback address, because the script is piped into a root shell.

The public endpoints are in `apps/esa/internal/api/enroll.go`. The token in the path is their only authentication:

| Endpoint                                       | Returns                                                                 |
| ---------------------------------------------- | ----------------------------------------------------------------------- |
| `GET /enroll/{token}`                          | The shell script.                                                       |
| `GET /enroll/{token}/helper?arch=amd64\|arm64` | The helper binary.                                                      |
| `POST /enroll/{token}/complete`                | Takes the server's host key as the body (up to 16 KiB). 204 on success. |

The script:

1. Wraps everything in one function called on the last line, so a download cut short runs nothing.
2. Picks the architecture from `uname -m`. The expected SHA-256 of each helper is written into the script.
3. Downloads the helper into a private `mktemp -d` directory and checks its SHA-256. A mismatch stops before anything is installed.
4. Runs the install script.
5. Reads `/etc/ssh/ssh_host_ed25519_key.pub`, or the ECDSA key if there is no ed25519 key, and posts it to `/complete`.

On `/complete`, the control plane connects to the server's registered address and port over SSH, asks for the same key type, and compares the key it gets with the one the script reported. Without this check, anyone holding the token could report a key for a host they do not control. If they match, it marks the token used, stores the key as accepted by the person who created the command, and sets the server to `ready`. Errors come back as `echo 'esa: ...' >&2; exit 1`, with status 410 for an unknown, used, or expired token.

### Path B: a one-time sudo login

This path is in `apps/esa/internal/fleet/server_add.go`.

1. `ProbeServer` connects to the address without logging in and returns the host key and its fingerprint. Compare it with `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` on the server.
2. `AddServer` takes the accepted key and a login: a user plus a password, or a PEM private key with an optional passphrase.
3. `remote.Install` connects with that login, pinned to the accepted key. It checks `uname -sm`, uploads the helper to `/tmp/esa-helper-upload-<random>` with umask 077, and runs the install script as root. As root it runs the script directly. Otherwise it tries `sudo -n`. If sudo needs the password, the script is uploaded as a file and the password is the only thing sent on stdin to `sudo -S`, so it never reaches a shell. The password is scrubbed from any error text.

The whole install has 5 minutes. Probing and adding are each limited to 20 calls per minute per account.

The login is used for this install and then discarded. With `keepLogin`, which only owners can set, it is sealed with `ESA_MASTER_KEY` into `server_credentials` and used for later helper reinstalls.

### After either path

The control plane runs `esa-helper version` as `esa` to check the key and sudo rule work, records the helper version and architecture, polls the server, and reads its pending updates.

## Host key pinning

Every SSH connection pins the server's accepted host key (`remote.Dial`). esa refuses to connect without one. It negotiates only that key's algorithm (RSA keys use `rsa-sha2-256`). If the server presents a different key, the handshake stops before any credential is sent or any command runs.

When that happens (`fleet.recordMismatch`), esa:

- Sets `host_key_mismatch` on the server's state, stores the presented key, and marks the server `failing` with the reason "host key changed".
- Drops the pooled connection, writes `server.host_key_mismatch` to the audit log, and opens a critical `host_key` alert.
- Stops all work on the server. Polls skip it, runs fail it, and every helper call is refused with a message to accept the new key first.

An admin of the owning account accepts the new key on the server page. The request must carry the fingerprint of the presented key, so the person confirms what they saw. Accepting stores the new key, clears the mismatch, and writes `server.accept_host_key` with the old and new fingerprints.

## Helper actions

`esa-helper <action> [flags]`. The control plane calls these through the `esa` key. Timeouts on the server are 3 hours for `upgrade-exec`, 15 minutes for `clean`, `updates`, `container-update`, and `image-check`, and 2 minutes for the rest.

### Actions that only read

| Action                             | Returns                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | Used by                               |
| ---------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------- |
| `version`                          | Helper version, commit, protocol, OS, architecture, and the list of actions this build has (`proto.Actions`). Works without root.                                                                                                                                                                                                                                                                                                                                                                                                | Enrollment, reinstall, and once a day |
| `status`                           | Disks, memory, CPU, load, network, time sync, reboot required, failed units, Docker summary.                                                                                                                                                                                                                                                                                                                                                                                                                                     | The poll every minute                 |
| `updates [--refresh]`              | Upgradable packages, which are security updates, held packages. `--refresh` runs `apt-get update` first.                                                                                                                                                                                                                                                                                                                                                                                                                         | Poll every 6 hours, "check updates"   |
| `upgrade-precheck`                 | Whether an upgrade can start, and why not.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | Update runs                           |
| `upgrade-status --run-id N`        | The result file of an upgrade.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | Update runs                           |
| `services`                         | Loaded systemd services, failed first, with a `protected` flag.                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | Server page                           |
| `containers [--protect a,b]`       | Containers with state, health, restart count, compose labels.                                                                                                                                                                                                                                                                                                                                                                                                                                                                    | Server page, reboots                  |
| `image-check`                      | Local image digest compared with the registry.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | Server page                           |
| `logs (--unit U \| --container C)` | Journal or container log lines. At most 1000 lines, default 200. `--grep` is cut to 200 characters.                                                                                                                                                                                                                                                                                                                                                                                                                              | Server page (needs `logs:read`)       |
| `access`                           | Users with a login shell, password state (never the hash), sudo rules, SSH settings, key fingerprints and restrictions, logins. Report revision 2. An older helper is reported as not ready. Key files it does not read are listed as gaps, not skipped: a symlinked or non-regular `authorized_keys`, lines it cannot parse, a custom `AuthorizedKeysFile`, an `AuthorizedKeysCommand`, SSH certificates (`TrustedUserCAKeys`), and `Match` blocks in `sshd_config`, whose settings `sshd -T` does not show.                    | Server page, Keys page                |
| `security`                         | Listening ports from `ss`, ufw match from `ufw status verbose` (port ranges, lists, app profiles from `/etc/ufw/applications.d`, and the default incoming policy), Docker publications from running containers, also when `ss` shows no proxy socket. A published port is not treated as protected by ufw, and that mark is used only when ufw is the firewall. A Docker error, an unknown app profile, or an unread default policy is a gap. Unattended-upgrades. CrowdSec decision count and fail2ban jail bans when those tools are installed. Banned addresses are not collected. | Server page                           |
| `jobs`                             | Cron entries and systemd timers.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | Server page                           |
| `recovery`                         | Restic and Borg result files the operator wrote in `/var/lib/esa/recovery`. Files are opened without following symlinks, and anything that is not a regular file (a FIFO, say) is refused without blocking. Esa does not run those tools, open a repository, or restore anything. A success is not proof the data can be restored. A backup older than 7 days is stale, a time in the future is flagged, and a sampled or structural check is not a full read. | Server page                           |
| `inventory`                        | Installed deb packages from `dpkg-query`, with the source package each was built from (Debian and Ubuntu advisories are keyed by source package). Only packages in the `installed` state count; removed packages that left config files do not. A `dpkg-query` failure is an error, not an empty list. No network.                                                                                                                                                                                                               | Advisory match                        |
| `clean --measure`                  | Reclaimable space per cleanup.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | `measure` runs                        |

`updates --refresh` changes nothing but apt's package lists.

### Actions that change the server

| Action                                                                                | Effect                                                                                                                                                                                                                                                                            |
| ------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `clean --apply [--only buildcache,vscode,journal] [--journal-size 200M]`              | Runs the selected cleanups.                                                                                                                                                                                                                                                       |
| `upgrade-start --run-id N --mode security\|all [--hold pkg,...] [--allow pkg,...\|-]` | Starts a package upgrade in a transient systemd unit. With `--allow`, installs only those packages (`-` is none), lists the rest as not approved, and installs nothing if apt would also upgrade a package that is not on the list or is held. A plan run always sends `--allow`. |
| `upgrade-exec`                                                                        | The body of that unit. Not meant to be called directly.                                                                                                                                                                                                                           |
| `service --unit U --action start\|stop\|restart`                                      | Acts on one systemd service.                                                                                                                                                                                                                                                      |
| `container --name C --action start\|stop\|restart [--confirm NAME]`                   | Acts on one container.                                                                                                                                                                                                                                                            |
| `container-update --name C [--confirm NAME]`                                          | Pulls a compose service's image and recreates that service.                                                                                                                                                                                                                       |
| `reboot`                                                                              | Reboots in 5 seconds.                                                                                                                                                                                                                                                             |
| `uninstall`                                                                           | Removes the `esa` user, its sudo rule, and the helper in 5 seconds.                                                                                                                                                                                                               |

## Safety rules

The helper enforces these rules itself, whatever the control plane asks. Breaking a rule returns the error code `refused`. Tests in `apps/esa/internal/hostops/safety_test.go` record every command the helper would run and check the rules.

### Services

Unit names must match `^[A-Za-z0-9][A-Za-z0-9@._:\-]{0,200}\.service$`, so only services are accepted. Actions run as `systemctl <action> --no-block <unit>`.

| Units                                                                                                                                                                                                                                                                                     | Rule                                                                  |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ssh`, `sshd`, `docker`, `containerd`, `systemd-journald`, `systemd-logind`, `dbus`, `systemd-resolved`                                                                                                                                                                                   | Can be restarted, never stopped.                                      |
| `networking`, `systemd-networkd`, `NetworkManager`, `ufw`, `nftables`, `iptables`, `ip6tables`, `netfilter-persistent`, `firewalld`                                                                                                                                                       | No start, stop, or restart.                                           |
| `systemd-poweroff`, `systemd-halt`, `systemd-reboot`, `systemd-kexec`, `systemd-soft-reboot`, `systemd-exit`, `systemd-suspend`, `systemd-hibernate`, `systemd-hybrid-sleep`, `systemd-suspend-then-hibernate`, `systemd-sysupdate-reboot`, `rescue`, `emergency`, `debug-shell`, `kexec` | Never started or restarted.                                           |
| Template instances such as `name@instance.service`                                                                                                                                                                                                                                        | Start and restart only when systemd already has that instance loaded. |

All names in the table carry the `.service` suffix.

### Containers

Container names and ids must match `^[a-zA-Z0-9][a-zA-Z0-9_.\-]{0,127}$`. Protected containers are matched by name prefix, ignoring case. The defaults are `traefik`, `authentik`, `portainer`, and `crowdsec`. A server can set its own list, which replaces the defaults; an empty list means the defaults. Every action on a protected container needs `--confirm` set to its exact name. Stop and restart give the container 30 seconds.

`container-update` has extra rules:

- The compose project and service names are checked against the protected list too, because compose labels can come from the image.
- The container must be managed by docker compose, with a project, a service, and a working directory label. Others are refused, because recreating them would need their full run options.
- The working directory must be an absolute path to a directory. Each compose file must be a regular file inside it.
- It runs `docker compose pull <service>` and `docker compose up -d --no-deps <service>`, 10 minutes each.

### Cleanups

Each cleanup is independent; one failing does not stop the others. Cleanups never prune images, volumes, or the whole system, and never restart Docker or containers.

| Cleanup      | What it removes                                                                                                                                                                                                                                                                             |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `buildcache` | Unused Docker build cache, the same as `docker builder prune -f` without `--all`. Skipped when a cache entry is in use or a build is running (`docker build`, `docker buildx build`, `docker compose build`, `bake`, `buildctl build`, and similar).                                        |
| `vscode`     | Old VS Code server copies under `.vscode-server/bin` and `.vscode-server/cli/servers` in `/root` and every home under `/home`. Keeps the newest copy and any copy a running process uses as its executable or working directory. Staging folders are removed only when older than one hour. |
| `journal`    | Runs `journalctl --vacuum-size=<size>`. The size looks like `200M` or `1G` and must be at least 50M. The default is `200M`.                                                                                                                                                                 |

For VS Code copies the helper never follows symlinks, and every directory from the home down must belong to the home's owner, so a user cannot point root at someone else's files. It checks the path again right before removing and removes through a handle on the checked directory.

## Updates

Only apt is supported. A server without `apt-get` answers `updates` with an error.

`upgrade-precheck`, and `upgrade-start` again before it starts anything, refuse when:

- apt, apt-get, dpkg, aptitude, or unattended-upgrade is running,
- an `esa-upgrade-*` unit is still active,
- `/` or `/var` has less than 1 GiB free, or `/boot` less than 150 MiB,
- `dpkg --audit` reports broken or half-installed packages.

`upgrade-start` then writes `/var/lib/esa/upgrade-<run id>.json` with state `running` and starts a transient unit:

```
systemd-run --unit=esa-upgrade-<run id> --collect \
  --property=KillMode=process --property=TimeoutStopSec=1h \
  --setenv=DEBIAN_FRONTEND=noninteractive --setenv=NEEDRESTART_MODE=l ... \
  /usr/local/lib/esa/esa-helper upgrade-exec --run-id <id> --mode <mode> [--hold ...]
```

`KillMode=process` keeps dpkg children from being killed mid-install. Inside the unit, `upgrade-exec` runs `apt-get update`, lists upgradable packages, drops held packages, and in `security` mode keeps only packages whose candidate version is published in a `-security` pocket. It installs them with `apt-get install -y -q --only-upgrade`, keeping existing config files (`--force-confdef`, `--force-confold`), within 2 hours. `NEEDRESTART_MODE=l` makes needrestart list services instead of restarting them. The result file gets the final state, each upgraded package with its version before (from apt's plan) and after (from `dpkg-query` once the install finished, empty if it cannot be read), the last 40 log lines, and whether `/var/run/reboot-required` exists.

`upgrade-status` reads the result file. If the file still says `running` but the unit has ended, it reports the upgrade as failed. The control plane polls this every 10 seconds; see [architecture.md](architecture.md#updates).

## Reboots

`reboot` starts a transient unit `esa-reboot-<unix time>` that runs `systemctl reboot` 5 seconds later, so the SSH command returns before the connection drops. The control plane then waits for the server to come back and checks its containers (see [architecture.md](architecture.md#reboots)).

## Upgrading the helper

There is no self-update action. With one, the `esa` key could install any binary as root; without it, the key can only run the helper's fixed actions. To upgrade the helper, reinstall it.

`ReinstallHelper` (the reinstall button on the server page) needs an admin, an accepted host key, and a sudo login: either one typed in for this reinstall, or the login kept with `keepLogin`. A server enrolled with path A has no kept login, so you type one in. Reinstalling runs the same install script over the pinned connection. It replaces the helper binary, rewrites `authorized_keys` with the account's current key, and rewrites the sudo rule. It then runs `esa-helper version` as `esa` and records the new version. Reinstalls are limited to 20 per minute per account.

## Removing the helper

Delete the server in esa with "Also remove esa from the server" switched on (it is on by default; the API takes `DELETE /api/v1/servers/{serverId}?uninstall=true`). The control plane calls `esa-helper uninstall` (30 second timeout). If that call fails, the delete is refused, so you can fix the connection or delete without removing.

`uninstall` starts a transient unit `esa-uninstall-<unix time>` that runs 5 seconds later, because the `esa` user's own SSH session would otherwise block `userdel`. It runs:

```sh
rm -f /etc/sudoers.d/esa /etc/sudoers.d/esa.new
if [ "$(getent passwd esa | cut -d: -f5,6)" = "easy-sys-admin control plane:/var/lib/esa-home" ]; then
  pkill -u esa || true
  sleep 1
  userdel --remove esa 2>/dev/null || userdel esa || true
  rm -rf /var/lib/esa-home
fi
rm -rf /var/lib/esa /usr/local/lib/esa
```

The `esa` user and its home are deleted only when the user carries esa's comment and home. Deleting a server without removing esa, or deleting an organization, leaves the user, the key, and the sudo rule on the server. To clean up by hand, run the script above as root.
