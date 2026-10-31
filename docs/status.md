# Status

What exists, how it was checked, and what is left. Update this page when a feature lands or a known gap is closed.

## Built and verified

Checked against a real Ubuntu 24.04 arm64 server unless noted.

- Enrollment by pasted one-time command and by one-time sudo login. A reused command is refused. A changed host key pauses all work until someone accepts a matching fingerprint.
- The hardened `esa` user: root-owned home, forced command on the key, sudo limited to the helper. Hostile commands through the forced command are refused.
- Live status every minute: CPU, memory, disks and inodes, load, network, time sync, reboot required, failed units, Docker. Hourly rollups; 48 hours raw, 90 days hourly.
- Updates: pending list with security detection, security-only or full upgrades with held packages. An upgrade survives `kill -9` of the control plane.
- Reboots: wait for a new boot time, then check that containers with a restart policy came back.
- Cleanups: Docker build cache (skipped during builds), old VS Code server copies, journal cap.
- Services, containers (including pulling and recreating one compose service), logs, access and security reports, scheduled jobs, image update checks.
- Groups with schedules. A slot runs once; a slot missed by over an hour is recorded as missed (tests).
- Alerts fire after their duration and resolve (tests). ntfy delivery. TLS certificate checks.
- API tokens with scopes, account isolation, and an audit log that marks web, API and MCP calls.
- Personal spaces, teams with invites and roles, and groups shared between teams with view or operate access (tests and a two-user browser run).
- The web app in a real browser at desktop and phone widths.
- CLI: login, status, servers, groups, runs (with `--wait`), alerts.
- MCP: tools follow scopes; `fleet_summary`, `list_groups`, `list_updates`, `start_run`.
- Deploy through GitHub Actions to a self-hosted runner, with secrets in the `production` environment.

## Known gaps

- Password reset by email.
- Email verification for password accounts. Email-restricted invites refuse password accounts for now.
- CLI commands for teams and sharing. They are in the web app and the API only.
- On a run shared with another team, the overall status can reflect servers that team cannot see.
- The helper protocol version is reported but not checked. A new helper action needs a reinstall on older servers.
- Rotating `ESA_MASTER_KEY` has no re-seal step, so old keys must stay in the list.
- The audit log is never pruned.
- A required reviewer on the GitHub `production` environment needs a paid plan while the repository is private.

## Not started

- An agent mode for servers without inbound SSH.
- RHEL-family servers (dnf).
- Following logs live.
- Configurable poll interval and retention (fixed at 1 minute, 48 hours and 90 days).
