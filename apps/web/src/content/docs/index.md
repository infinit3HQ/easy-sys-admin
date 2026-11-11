---
title: Introduction
description: What Easy Sys Admin is, and how the hosted beta, the CLI, and MCP fit together.
---

Easy Sys Admin is a board for a small fleet of Linux servers. You add the servers you administer, put them in groups, and choose when each group cleans its disks and installs package updates. The board shows live status. You can share a group with another organization. The same data is available to the `esa` command and to AI tools over MCP.

Nothing from Easy Sys Admin listens on those servers. The control plane connects out over SSH as a restricted `esa` user. That user can run only one small helper program.

The hosted public beta is at [easysysadmin.app](https://easysysadmin.app). Sign in with Google or with email and a password. It is free. Each space can hold 10 servers, 20 in total across your personal space and one organization. [Plans](/pricing) lists what comes next. Nothing is charged.

## What you can do

- See CPU, memory, disks, inodes, load, network, time sync, failed units, Docker, and whether a reboot is required. The board keeps history for 90 days.
- Install package updates, security updates only or all of them, in batches, and stop after a set number of failures. An upgrade runs in a systemd unit, so a dropped connection does not leave `apt` half finished.
- Clean idle Docker build cache, old VS Code server copies, and the journal, on a schedule. The helper does not delete volumes or images a container is using.
- Read services, containers, logs, scheduled jobs, and access and security reports. The board keeps a short exposure summary: public ports, SSH login settings, the firewall, and counts of extra root accounts and unrestricted sudo rules.
- Send alerts for disk, inodes, memory, unreachable servers, failed units, unhealthy containers, certificates, and failed runs. An exposure alert for weak SSH, a missing firewall, or unrestricted sudo is off until you turn it on. Delivery is ntfy, a webhook, Discord, Slack, or email.
- Use a personal space, organizations with owner, admin, and viewer roles, extra teams, and groups shared between organizations.

## Three ways in

The web app, the `esa` CLI, and the MCP endpoint all use one HTTP API. A browser uses a session cookie. The CLI and MCP use an API token you create under Settings.

| You want to                                                 | Start here                    |
| ----------------------------------------------------------- | ----------------------------- |
| Add your first server on the hosted beta                    | [Quickstart](/quickstart)     |
| See what runs on a server, and what it is not allowed to do | [How it works](/how-it-works) |
| Look up a command or an MCP tool                            | [CLI](/cli), [MCP](/mcp)      |
| Call the HTTP API                                           | [API reference](/api)         |
| Run the control plane yourself                              | [Self-hosting](/self-hosting) |

Self-Hosted is the same software, run by you. The license sets no usage cap. The default is 500 servers per account, and that number is a setting. The source is [github.com/infinit3HQ/easy-sys-admin](https://github.com/infinit3HQ/easy-sys-admin). Managed Cloud, where we host it for you, is coming soon. See [Plans](/pricing).
