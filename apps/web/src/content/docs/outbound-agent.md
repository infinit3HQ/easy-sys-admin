---
title: Outbound agent
description: Connect a server to esa without incoming SSH.
---

Use the outbound agent when a server cannot accept incoming SSH from esa. The server needs outgoing HTTPS to your esa origin. Supported hosts use systemd, sudo, and either Ubuntu 24.04 or Rocky Linux 9, on amd64 or arm64. Rocky needs `dnf-utils` for reboot checks.

## Install

1. Sign in again, then choose **Add a server > Outbound agent**. Only a space admin can register a server. Pick its name and group.
2. Copy the one-time credential and the signing pin shown with it. esa stores only a hash of the credential. The credential expires after 365 days. The pin is public information, but it must reach the server by a path that does not go through esa, such as your terminal.
3. Download and inspect the installer on the server. Replace the example origin with your own. The installer installs or replaces esa-helper and esa-agent, creates the restricted esa user if needed, installs its helper-only sudo rule, and starts a systemd service. It keeps existing SSH settings and keys.

```sh
curl --proto '=https' --tlsv1.2 -fsS https://esa.example.com/enroll/outbound/install.sh -o esa-agent-install.sh
less esa-agent-install.sh
sudo sh esa-agent-install.sh https://esa.example.com
```

The installer prompts for the credential with terminal echo off, then for the signing pin. It prints the SHA-256 of the helper and agent it downloaded. Compare them with the hashes in the release notes, or pin them in advance with `sudo ESA_HELPER_SHA256=<hash> ESA_AGENT_SHA256=<hash> sh esa-agent-install.sh https://esa.example.com`. The server that sends the binaries cannot vouch for them, so the pinned hashes must come from the release notes, not from your esa. It never passes it as a process argument or puts it in a URL. Run it from a terminal, not a pipe. The host must already have `curl`, `sudo`, `visudo`, `useradd`, and systemd. The script refuses unsupported architectures and symlinks at its installation paths.

The agent runs as `esa`. Its only sudo permission is the existing fixed helper executable. The helper and agent binaries are owned by root. `/etc/esa/agent.token` is root:esa, mode 0640. `/etc/esa/agent.env` is root-only and contains:

```ini
ESA_AGENT_URL=https://esa.example.com
ESA_AGENT_TOKEN_FILE=/etc/esa/agent.token
ESA_AGENT_PIN=esa_pin_...
```

The pin names the server and the key that signs its commands. esa signs every command with the account key, and the agent runs only commands whose signature matches its pin and its server. A proxy, TLS terminator or CDN in front of esa that starts rewriting traffic after setup can read it but cannot give the agent work, and a command that is replayed or delivered late is refused because ids only rise (the mark lives in memory, so a restart forgets it and the command expiry, at most ten minutes, bounds what could be replayed). Something that is already rewriting traffic while you set up can change the installer and the pin shown to you, so check the installer against the hash in the release notes (`sha256sum esa-agent-install.sh`). An agent installed before this check has no pin and logs that commands are not verified; reinstall it with a pin. The agent sends no data anywhere else. It verifies HTTPS normally and refuses redirects. Numeric loopback HTTP is allowed for isolated development tests. There is no TLS verification bypass.

## Use and rotate

The board, CLI, MCP, plans, and schedules use the same API and helper permissions as SSH servers. The server's settings show **Outbound HTTPS**. The agent polls every two seconds while connected and backs off to one minute after failures. No inbound listener or tunnel is opened.

In **Settings > Outbound connection**, choose **Replace credential**. A sign-in within 15 minutes is required. Replace the token file locally and restart `esa-agent.service`. The old credential stops working immediately. Rotation and **Disable connection** cancel pending work. An operation already dispatched can still finish on the server. Revocation never switches a server back to SSH.

Use `systemctl status esa-agent.service` to check the process. Credentials and helper output are never printed by the agent. Check the board for missing results. There is no automatic helper update; rerun the reviewed installer explicitly to install a new helper.

## Failure and delivery rules

A credential identifies exactly one server. It cannot call the user API, select another server, approve a plan, or submit an unclaimed result. It expires after a year. A revoked or rotated generation cannot claim work or return results.

Commands live in PostgreSQL, so any control plane can receive the agent's poll or result. Each claim is atomic and is delivered at most once. A command expires after at most ten minutes, often sooner because the requesting operation has a shorter deadline. Commands that were not claimed are cancelled when the caller stops waiting. A claimed command may already be running; cancellation cannot undo a dispatched host change.

A result retry keeps the first result. The agent never reruns a command to resend its result. If the agent restarts or a result is lost, the operation stays unknown or interrupted. Upgrade recovery follows its durable helper result. Outbound upgrades require a helper advertising `upgrade-idempotent`; repeating a run ID cannot install it again. The agent refuses the helper's internal `upgrade-exec` action and self-uninstallation.

Arguments and results are encrypted at rest with `ESA_MASTER_KEY` and bound to their task ID. Payloads are erased after consumption or cancellation; expired receipts are deleted each minute. A helper result is limited to 4 MiB. Oversized output is a failure, never a complete report. Master-key re-sealing includes outstanding task payloads. Drain active tasks or run the re-seal check before removing an old key.

## Remove

Stop and disable the service locally first:

```sh
sudo systemctl disable --now esa-agent.service
```

Disable its credential in esa, then remove the server from the board. Outbound connections cannot uninstall their own account. Remove `/etc/systemd/system/esa-agent.service`, `/etc/esa/agent.env`, `/etc/esa/agent.token`, and `/usr/local/lib/esa/esa-agent` locally. Follow the helper removal instructions if the server no longer uses esa over SSH either. Removing the board record deletes its credential and tasks.
