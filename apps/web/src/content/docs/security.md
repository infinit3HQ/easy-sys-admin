---
title: Security
description: How to report a vulnerability, and where the security model is described.
---

Please do not open a public issue for a security problem. Open a private security advisory on the GitHub repository: the Security tab, then Report a vulnerability. Include what you found, how to reproduce it, and the commit you tested.

Only the latest commit on `main` gets fixes.

The questions address in the repository security policy is the same place. There is no public bug-bounty program described in the project.

What the product does to keep the control plane and the servers apart is summarized in [How it works](/how-it-works). The longer notes for people changing the code are `docs/security.md` and `SECURITY.md` in the source tree. Those cover secrets at rest, sign-in, sessions, tokens, MCP, CSRF, outbound addresses, rate limits, and the audit log.
