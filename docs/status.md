# Status

What exists, how it was checked, and what is left. Update this page when a feature lands or a known gap is closed.

## Built and verified

Checked against a real Ubuntu 24.04 arm64 server unless noted.

- Enrollment by pasted one-time command and by one-time sudo login. A reused command is refused. A changed host key pauses all work until someone accepts a matching fingerprint.
- The hardened `esa` user: root-owned home, forced command on the key, sudo limited to the helper. Hostile commands through the forced command are refused.
- Live status every minute: CPU, memory, disks and inodes, load, network, time sync, reboot required, failed units, Docker. Hourly rollups; 48 hours raw, 90 days hourly by default (`ESA_METRICS_RAW_RETENTION`, `ESA_METRICS_HOURLY_RETENTION`).
- Polling at scale: 64 servers at once by default (`ESA_POLL_CONCURRENCY`), servers that stop answering polled every 5 minutes, a 5 second TCP connect timeout, and a warning when a cycle runs out of time. Measured with a load test against in-process SSH servers (`internal/fleet/poll_load_test.go`), not a real fleet.
- `GET /metrics` on the control plane in the Prometheus text format (tests).
- Updates: pending list with security detection, security-only or full upgrades with held packages. Update and reboot runs work in batches of a group-set size and stop after a set number of failed servers. An upgrade survives `kill -9` of the control plane, and a restarted control plane follows every upgrade of the batch in flight.
- Reboots: wait for a new boot time, then check that containers with a restart policy came back.
- Cleanups: Docker build cache (skipped during builds), old VS Code server copies, journal cap.
- Services, containers (including pulling and recreating one compose service), logs, access and security reports, scheduled jobs, image update checks.
- A short note on each server, kept in the control plane. A group lists where its servers differ on password login, root login, firewall, and sockets listening on all interfaces. The status tab estimates how soon the fullest disk fills. A Monday summary goes to the space's channels when there is something to report. The board counts servers whose helper is too old for the exposure summary.
- Exposure summary on the board, collected about once an hour and at most 8 servers per poll cycle. It stores sockets listening on all interfaces, sockets on one non-loopback address, SSH flags, the firewall name, and two counts. That list is not a reachability check. A port Docker publishes is not treated as covered by ufw. The exposure alert is off until an admin enables it. An older helper is reported as not ready rather than clean. A new socket on all interfaces, or a new SSH key, sends one notice after the first report. A security update that is still pending after 7 days alerts, and that condition is also a review item. ufw coverage is on the security report. fail2ban bans and CrowdSec decision counts are included only when that server has the tool. Banned addresses are not stored. A missing tool is not a finding. An admin can approve the keys currently recorded on a server; later checks report additions, removals, and restriction changes, and do not claim access was removed. The Keys page can look up one name and says the same limit. A maintenance window quiets ordinary notices for at most 7 days. Host key changes and new SSH keys are still sent. After a package upgrade, a new failed unit or a rise in unhealthy containers fails that server so the batch can stop. A problem that was already there does not. A group can add TCP, HTTP, service, and container checks that run before and after the upgrade. Canaries run first, and one failed canary skips the rest. A watch repeats those checks for up to 10 minutes before the other servers start. The run report lists packages, checks, and skip reasons. A token can propose a typed update, cleanup, or measure plan. Only a browser session can approve it, for 30 minutes. A token that can read logs cannot run it, and a second execute returns the same run. A group can require a plan for an interactive update or cleanup. The schedule still runs. An update plan freezes the recorded package list, and a different list invalidates the approval. A server page can show restic or Borg result files an operator wrote under `/var/lib/esa/recovery`. Esa does not run those tools. A success in that file is not proof the data can be restored.
- Groups with schedules. A slot runs once; a slot missed by over an hour is recorded as missed (tests).
- Alerts fire after their duration and resolve (tests). ntfy delivery. TLS certificate checks.
- API tokens with scopes, account isolation, and an audit log that marks web, API and MCP calls.
- Optional system SMTP (`ESA_SMTP_*`) for password confirmation and reset. Messages are HTML, with a plain-text copy. A password account cannot sign in until it opens the link. Google sign-in does not, and the same verified email joins the existing account. Off unless the host is set. Sends are limited to 3 an hour per address and 10 an hour per client.
- Personal spaces, organizations with owner, admin, and viewer roles, extra teams that grant admin on chosen groups, invites, and groups shared between organizations with view or operate access (tests in `teams_test.go`, `roles_test.go`, `sharing_test.go`, and `isolation_test.go`). `ESA_BETA_LIMITS` (off by default) caps a user at one organization besides their personal space and each account at 10 servers.
- The web app in a real browser at desktop and phone widths. `ESA_SEO` (off by default) publishes `robots.txt` and `/sitemap.xml` for the landing page, pricing, privacy policy, terms, and `/docs`. A self-hosted install leaves it off.
- A plans dialog and a `/pricing` page. Nothing is charged. Self-Hosted is available from the source. Managed Cloud is marked coming soon. The numbers live in `packages/plans`.
- Public docs at `/docs` in the web app, rendered from `apps/web/src/content/docs`.
- The source tree is under AGPL-3.0-only. The web footer links to the source and shows the running version and commit (`ESA_SOURCE_URL`, `ESA_VERSION`, `ESA_COMMIT`).
- Draft Terms of Service and Privacy Policy for the hosted beta, in `TERMS.md` and `PRIVACY.md`, rendered at `/terms` and `/privacy`.
- CLI: login, status, servers, groups, runs (with `--wait`), alerts.
- MCP: tools follow scopes; `fleet_summary`, `list_groups`, `list_updates`, `start_run`.
- Deploy through GitHub Actions to a self-hosted runner, with secrets in the `production` environment.

## Known gaps

- CLI commands for organizations, teams, and sharing. They are in the web app and the API only.
- On a run shared with another organization, the overall status can reflect servers that organization cannot see.
- A helper that speaks a protocol other than 1 is refused. Report revision 2 is required for a complete access or security report. An older helper is shown as not ready, and its last complete exposure summary is kept. SSH key fingerprints from a current helper are listed on the Keys page. The list is not every way to log in.
- Rotating `ESA_MASTER_KEY` has no re-seal step, so old keys must stay in the list.
- The audit log is never pruned.
- A required reviewer on the GitHub `production` environment needs a paid plan while the repository is private.

## Not started

- An agent mode for servers without inbound SSH.
- RHEL-family servers (dnf).
- Following logs live.
- A configurable poll interval (fixed at 1 minute).

## Capacity

Measured with `TestPollLoad` (`apps/esa/internal/fleet/poll_load_test.go`): in-process SSH servers that answer `status` after 1.2 seconds, like a real server, and 55 second cycles.

| Healthy servers |        Dead servers | Concurrency | Polled in one cycle | Cycle time |
| --------------: | ------------------: | ----------: | ------------------- | ---------: |
|             300 |                   0 |           8 | all 300             |     46.0 s |
|           1,000 |                   0 |           8 | 360                 |     55.0 s |
|           1,000 |                   0 |          64 | all 1,000           |     19.6 s |
|           1,000 | 50, before back-off |          64 | all 1,000           |     37.0 s |
|           1,000 |  50, after back-off |          64 | all 1,000           |     19.6 s |

Concurrency 64 is the default. Going by the 1,000-server result, one control plane should poll about 2,500 servers a minute at that setting (not measured beyond 1,000); raise `ESA_POLL_CONCURRENCY` for more. Only one control plane process may run, because run locks, rate limits, the notification queue and poll back-off live in memory. The board loads every server of a space every 15 seconds, which is fine at the default cap of 500 servers per space.
