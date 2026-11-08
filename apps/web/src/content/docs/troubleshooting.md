---
title: Troubleshooting
description: Enrollment, host keys, sign-in, the CLI, MCP, and servers that stop answering.
---

## The enroll command failed

The token lasts 15 minutes and works once. Ask for a new command and paste that one. A second paste of the same command is refused.

The script needs `curl`, `sha256sum` or `shasum`, `sudo`, and root. It stops if the SHA-256 of the helper does not match. It also stops if a user named `esa` already exists and is not the one Easy Sys Admin created.

The control plane will not issue the command when `ESA_PUBLIC_URL` is plain `http` and the host is not loopback.

Only amd64 and arm64 are supported. Another architecture has no helper binary.

## The host key does not match

On the server:

```sh
ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
```

Accept the new key in the board only after you have compared that fingerprint. Until you do, polls and runs skip the server. That pause is deliberate.

## A password account cannot sign in

When system mail is configured, a password account stays closed until you open the confirmation link. Check the mailbox, including spam. Google sign-in does not use that link.

If mail is not configured, a password sign-up for an address that already uses Google is refused. Sign in with Google.

Sign-up can also be limited to an allow list. An address that is not on the list cannot sign in.

## The server is unreachable

The control plane connects out to the SSH port you entered. Check that port from the control plane's host, that the server is up, and that a firewall allows the connection. A server that stops answering is retried every 5 minutes. The TCP connect waits 5 seconds.

A changed host key looks like a failure, not a timeout. The reason on the board says the host key changed.

## A run failed on apt

Updates use `apt` on Debian and Ubuntu. Another family is not supported. An upgrade that was killed in the past can leave dpkg in a state that needs a person on the server. The helper will not run a shell for you.

Held packages are skipped. Security-only mode installs packages whose candidate version is in a `-security` pocket.

## The CLI says it is not logged in

Run `esa login <url> --token esa_...`, or set both `ESA_URL` and `ESA_TOKEN`. The token must start with `esa_` and must not be revoked or expired. Plain `http` is refused by `esa login` except on localhost.

## MCP returns 401 or 403

401 means the bearer token is missing, invalid, expired, or revoked. Browser cookies are not accepted.

403 with a message about cross-origin requests means the client sent an `Origin` header for a different site. Command-line clients do not send one.

`start_run` missing from the tool list means the token can read logs. Make a second token with `actions` and without `logs:read`.

## You hit the server cap

On the hosted beta each space holds 10 servers, 20 in total. Deleting a server frees a slot. The cap does not delete servers you already have. It only blocks new ones.

## Reinstalling the helper asks for a login

A server added with the one-time command does not keep a sudo login. Reinstall asks you to type one. A server added with "keep login" can reuse the stored login. Only an owner can keep a login.
