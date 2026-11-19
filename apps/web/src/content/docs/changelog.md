---
title: Changelog
description: Release notes. The public beta is the current release.
---

Versioned release notes will be added here as releases are cut. The hosted public beta is the current release. There is no numbered release yet.

## Unreleased

- Plans page and plans dialog. Nothing is charged. Self-Hosted is available from the source. Managed Cloud is marked coming soon.
- Documentation at `/docs`.
- Stricter account linking, approval rules, service safety checks and private-network defaults. See Security for what changed.
- Breaking default: `ESA_ALLOW_PRIVATE_NETWORKS` is now off when a Google or OIDC provider is set without `ESA_ALLOWED_EMAILS`. Installs that manage servers or send mail through private addresses must set it to `true`.
- The source tree is under AGPL-3.0-only. The web app links to the source of the running build.

## Coming soon

- Managed Cloud.
- Release builds of the `esa` binary.
