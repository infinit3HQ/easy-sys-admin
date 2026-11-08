---
title: How it works
description: The control plane, the restricted esa user, host key pinning, and what never runs on a server.
---

```text
browser, CLI, or AI tool
        |
        | HTTPS
        v
web app (board, /mcp, and a proxy)
        |
        v
control plane (Go) ---- PostgreSQL
        |
        | SSH as the user esa
        v
esa-helper on each server
```

The web app holds no fleet data and makes no permission decision. The control plane does both. On the hosted beta, one hostname serves the board, the API, sign-in, MCP, and the enroll scripts.

The control plane opens SSH to your servers. Your servers do not open a port for Easy Sys Admin, and the helper does not listen on the network.

## The esa user

Enrollment creates a system user named `esa`. Its home is `/var/lib/esa-home`, owned by root, so `esa` cannot write there. Its shell is `/bin/sh`. The password field is `*`, which is not a usable password and does not block key login.

Each space has one ed25519 key pair. The private key is encrypted in the database. The public key is the only line in `authorized_keys`, and it is forced:

```text
restrict,command="/usr/local/lib/esa/esa-helper ssh-dispatch" ssh-ed25519 AAAA... esa-control-plane
```

`restrict` turns off port forwarding, agent forwarding, X11 forwarding, and a terminal. Whatever command the client asks for, sshd runs the dispatcher.

The dispatcher accepts only this shape, with no shell:

```sh
sudo -n /usr/local/lib/esa/esa-helper <action> ...
```

Anything else is refused. The helper then runs as root through the single sudo rule:

```text
esa ALL=(root) NOPASSWD: /usr/local/lib/esa/esa-helper *
```

Programs the helper starts get a fixed `PATH`. They do not inherit the caller's environment. Docker is reached through its engine socket, not a shell.

## Host key pinning

Every SSH connection requires the host key that was accepted at enrollment. The control plane offers only that key's algorithm. If the server presents a different key, the handshake stops before any credential is sent and before any command runs.

When that happens:

- The server is marked failing, with the reason "host key changed".
- Polls skip it, runs fail it, and helper calls are refused.
- A critical alert is opened.
- An admin of the owning space accepts the new key on the server page. The request must carry the fingerprint that was presented, so the person confirms what they saw.

Compare a fingerprint on the server with `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`.

## What the helper will not do

These rules are enforced in the helper, not only hidden in the board:

- Cleanups never delete Docker volumes, never remove an image a container is using, and never run `docker system prune`.
- Build-cache cleanup is skipped while a build is running, and it does not prune the whole cache.
- Cleanups never stop or restart Docker or containers.
- `ssh`, `sshd`, Docker, containerd, and a short list of other system units can be started or restarted, and cannot be stopped.
- Network and firewall units are never started, stopped, or restarted from here.
- A reboot started by a schedule skips the server that hosts the control plane.

The full list of actions is on the [Features](/features) page. Removing the user, the sudo rule, and the helper is in [Remove a server](/remove-a-server).

## Accounts and tokens

A personal space is private to you. An organization has owners, admins, and viewers. Extra teams grant admin on chosen groups without making someone an admin of the whole organization. A group can be shared with another organization as view or operate. The API is the real check. The board hides controls you cannot use.

API tokens are for the CLI and for MCP. Scopes are `read`, `logs:read`, `actions`, and `admin`. Every token has `read`. A token expires. The secret is shown once. The database stores a hash.

Browser sessions last 24 hours without use, or 7 days, whichever comes first. The cookie is `HttpOnly`.

Passwords are hashed. Server logins you choose to keep, the space's SSH private key, and notification credentials are encrypted with `ESA_MASTER_KEY`. That key is not in the database. Without it, those secrets cannot be read.

## What is not done yet

- One control plane process. Run locks, rate limits, the notification queue, and poll back-off live in memory, so a second process is not supported.
- Package updates are `apt` only.
- The helper's protocol version is reported and not checked. A new helper action needs a reinstall on older servers.
- There is no step that re-encrypts old secrets when you rotate `ESA_MASTER_KEY`. Keep old keys in the list.
- The audit log is not pruned.
- There is no agent for servers that cannot accept inbound SSH.
- Logs are not followed live. The poll interval is fixed at one minute.
