---
title: Quickstart
description: Sign up on the hosted beta, create a space, and enroll a server.
---

This page is the hosted public beta at [easysysadmin.app](https://easysysadmin.app). Self-hosting is a separate guide, and the unrestricted public release is [coming soon](/self-hosting).

You need a Linux server you administer, with SSH and `sudo`. The enroll command also needs `curl` and `sha256sum` (or `shasum`). Supported architectures are amd64 and arm64. Package updates use `apt`, so this is Debian and Ubuntu. RHEL-family servers are not supported yet.

## 1. Sign in

Open [easysysadmin.app](https://easysysadmin.app) and sign in with Google, or with an email and a password.

A password account cannot sign in until you open the confirmation link, when the install is sending mail. Google sign-in does not need that link. The same verified email joins the account that already uses it.

You land in a personal space. That space is private. It is created the first time you sign in.

## 2. Create an organization, if you want one

The personal space is enough to start. On the hosted beta you can also own one organization. Switch to it from the space menu in the sidebar. People you invite join as viewers until you change their role.

The caps on the hosted beta are 10 servers in each space, so 20 servers across the personal space and that one organization.

## 3. Add a server

On the board, choose Add server. You need the admin role. There are two ways. Both pin the server's host key. Neither leaves a program listening on the server.

### One-time command

The board gives you a command like this:

```sh
curl -fsSL https://easysysadmin.app/enroll/enr_... | sudo sh
```

Run it on the server as a user who can use `sudo`. The token starts with `enr_`, lasts 15 minutes, and works once. Asking for a new command cancels the previous one.

The script downloads the helper, checks its SHA-256, installs it, and reports the server's host key. The control plane then connects over SSH and checks that the key matches. If it does, the server is ready.

The control plane refuses to hand out this command when its public URL is plain `http` and not a loopback address, because the script is piped into a root shell. The hosted beta uses `https`.

### One-time sudo login

Use this when the server cannot reach the control plane, or when you would rather type a login than paste a script.

1. Choose Check host key. Easy Sys Admin reads the server's SSH host key. Nothing is sent to the server yet.
2. On the server, compare the fingerprint with:

   ```sh
   ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
   ```

   If the server has no ed25519 host key, compare the ECDSA key instead.

3. Enter a user that is root, or a user that can use `sudo`, and a password or a PEM private key. The install uses that login and then discards it.
4. Keep the login only if you want later helper reinstalls to reuse it. Only an owner can turn that on. The login is encrypted with the install's master key.

The whole install has 5 minutes. After either path, the control plane runs the helper once to confirm the key and the sudo rule, then polls the server.

## 4. Put it in a group

A group holds the schedule. Put the server in a group and choose when it cleans disks and installs updates. A schedule that is off does not run.

The first poll fills in status within about a minute. Updates are checked on a slower cycle. Until the first check, the updates list says it has not been checked yet.

## What got installed

The server now has a system user named `esa`, a helper at `/usr/local/lib/esa/esa-helper`, and one sudo rule:

```
esa ALL=(root) NOPASSWD: /usr/local/lib/esa/esa-helper *
```

The SSH key for `esa` is forced to run the helper's dispatcher. Details, and how to remove it, are in [How it works](/how-it-works) and [Remove a server](/remove-a-server).
