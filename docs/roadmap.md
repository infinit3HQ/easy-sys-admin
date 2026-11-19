# Roadmap

The implementation roadmap and its supported scopes. [status.md](status.md) records behavior and verification. Update a row in the same change that lands its work. Add a new row when a new requirement is accepted.

Last checked against the code: 18 November 2026, branch `security-watch`.

## Rules for every item

- Self-hosting stays free, with every feature, under AGPL-3.0-only. No telemetry, license check, or phone-home. Feeds and probes are off until an operator turns them on.
- Unknown, stale, absent, and clean are different results. A failed or partial check never shows as clean, and never resolves a finding.
- Changes to a server go only through typed `esa-helper` actions. No shell, no automatic firewall or key changes, no helper self-update.
- The API checks every permission. Space, group role, and token scope apply to reads, writes, exports, and runs.
- An agent can propose. Only a person in the web app can approve.

## Status

All 15 items are implemented in the supported scopes below. This does not mean every Linux release, historical binary, or production deployment has been tested. [status.md](status.md) and [development.md](development.md) record the verification and limits.

| Item | What it is                     | Status                 | Supported scope                                                                                                                                                                              |
| ---- | ------------------------------ | ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P0   | Truthful exposure labels       | Done                   | Listening sockets are separate from reachability.                                                                                                                                            |
| C0   | Shared evidence model          | Done                   | Attempt, last success, source, completeness, gaps, and age stay separate.                                                                                                                    |
| F1   | Security inbox                 | Done                   | Assignments, history, recurrence, fresh-evidence resolution, delivery retries, and protected urgent notices.                                                                                 |
| F2   | Exposure and reachability      | Done                   | Passive evidence and operator-run TCP probes. No default public probe service or firewall changes.                                                                                           |
| F3   | SSH key owners and offboarding | Done                   | Versioned baselines, current evidence, identity labels, and offboarding reports. No automatic key removal.                                                                                   |
| F4   | Health checks and canaries     | Done                   | Before/after checks, canaries, watch, restart-safe person gate, reasoned overrides, and package results.                                                                                     |
| F5   | Approved plans                 | Done                   | Package subsets, recent-person approval, optional two-admin approval, frozen requirements, and current membership checks.                                                                    |
| F6   | Vulnerability matching         | Done                   | Grype is the supported optional scanner. Actual Trivy comparison is recorded. Operators supply scanners and feeds; esa redistributes neither.                                                |
| F7   | Backup and restore evidence    | Done                   | Separate backup, repository, restore-test, and smoke-test evidence with age and idempotent submission. A restore engine is outside scope.                                                    |
| G1   | Helper compatibility           | Done                   | Protocol, actions, features, and report revision. Compatible older helpers remain supported; updates are explicit.                                                                           |
| G2   | Re-seal stored secrets         | Done                   | Dry run, per-class counts, and row-bound re-sealing, including transient agent tasks.                                                                                                        |
| G3   | Retention                      | Done                   | Optional audit and report retention, legal hold, prune ledger, preserved current evidence, and published hosted policy. Pruning defaults off.                                                |
| G4   | Multiple control planes        | Done                   | Shared ownership and claims, active lock-connection loss tests, recovery after install dispatch, and an actual baseline/current binary matrix.                                               |
| G5   | Outbound servers               | Done                   | HTTPS polling, server-bound credentials, encrypted expiring tasks, claim-once delivery, explicit installer, and durable upgrade idempotency. Live read-only playground connection verified.  |
| G6   | RHEL-family support            | Done for Rocky Linux 9 | RPM inventory, security metadata, approved DNF transactions, signature refusal, version confirmation, and reboot checks. Native ARM64 container verified. Other releases remain unsupported. |

## Completed order and decisions

The remaining inbox, approval, probe, scanner, retention, and multi-control-plane work landed before outbound transport and Rocky support. The owner delegated product decisions on 17 November 2026. [decisions.md](decisions.md#defaults-for-remaining-roadmap-work) records those defaults.

The supported scanner is optional Grype, supplied by the operator. [scanner-comparison.md](scanner-comparison.md) records the fixture, database dates, licence boundary, and different match counts. No scanner or feed is bundled.

Outbound transport supports systemd Linux hosts on amd64 and arm64 with outgoing HTTPS. Rocky support is deliberately limited to release 9 with DNF 4 and `dnf-utils`. Package operations were verified in an isolated container on the playground; a Rocky VM boot/reboot, every package dependency combination, and a production rollout are not claimed. Finish upgrading all control planes before enabling new outbound servers.

There is no queued implementation item in this roadmap. New requirements belong in a new row with a concrete supported scope.

## Not planned

A general firewall solver, an exploit scanner, a backup engine, key revocation on servers, a directory or HR sync, and any claim of complete discovery, compliance, or "secure".
