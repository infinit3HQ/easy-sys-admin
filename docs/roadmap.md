# Roadmap

What comes next, in order, and why. [status.md](status.md) says what is built today; this page says what is left. Sizes are rough engineer-weeks of focused work, not dates. Update a row in the same change that lands the work, and move finished detail to `status.md`.

Last checked against the code: 13 November 2026, branch `security-watch`.

## Rules for every item

- Self-hosting stays free, with every feature, under AGPL-3.0-only. No telemetry, license check, or phone-home. Feeds and probes are off until an operator turns them on.
- Unknown, stale, absent, and clean are different results. A failed or partial check never shows as clean, and never resolves a finding.
- Changes to a server go only through typed `esa-helper` actions. No shell, no automatic firewall or key changes, no helper self-update.
- The API checks every permission. Space, group role, and token scope apply to reads, writes, exports, and runs.
- An agent can propose. Only a person in the web app can approve.

## Status

| Item | What it is                                                                    | Status                   | Left to do                                                                                                                                                                                                                                                                                               |
| ---- | ----------------------------------------------------------------------------- | ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P0   | Truthful exposure labels                                                      | Done                     | Nothing.                                                                                                                                                                                                                                                                                                 |
| C0   | Shared evidence model: observed-at, source, completeness, failure reason, age | Not started              | Agree the model, then use it in F1 to F3. Today each report keeps its own list of gaps.                                                                                                                                                                                                                  |
| F1   | Security inbox                                                                | Partial                  | Open and resolved states, resolved only by fresh evidence. Accepted-risk exceptions with an expiry. Notes and an assignee. Timeline and evidence view. Routes by severity and a daily digest. A visible list of failed deliveries. MCP tools to read notices.                                            |
| F2   | Exposure states and reachability checks                                       | Passive part mostly done | Per-listener facts (bind, firewall policy, private and external checks) with an age each. Opt-in TCP probes from a named place, owner-approved and limited.                                                                                                                                              |
| F3   | SSH key owners, baselines, offboarding                                        | Partial                  | People and service accounts with active, leaving, and left states. Versioned baselines that cover users, sudo, and SSH settings too. Exceptions. An offboarding report to export. Flag `AuthorizedKeysCommand` and SSH certificates as gaps.                                                             |
| F4   | Health checks and canaries for updates                                        | Mostly done              | Pause the run at the canary gate and let a person continue it. An override with a reason. Versioned check profiles. Package versions before and after in the report.                                                                                                                                     |
| F5   | Plans a person approves                                                       | Core done                | The helper installs only the approved packages and stops if apt would add one. Left: plan versions and cancel, signing in again before approving, an optional second approver (open decision), and showing not-approved packages in the patch report.                                                    |
| F6   | Vulnerability matching                                                        | Stand-in only            | Pick one offline scanner (Grype or Trivy) after testing on our distros. Feed import with an age limit. Container images. Ranking by known exploitation. Exceptions.                                                                                                                                      |
| F7   | Backup and restore evidence                                                   | Stand-in only            | Datasets and their policies. Separate backup, repository check, and restore test results. An endpoint to send results to. Optional warning before an update.                                                                                                                                             |
| G1   | Helper capability checks                                                      | Mostly done              | The helper lists its actions and the control plane refuses any other, with `helper_incompatible`; runs check every server first; the server page shows what a reinstall adds. Left: a "needs reinstall" filter on the board, and a rule for how many old helper versions stay supported (open decision). |
| G2   | Re-seal stored secrets                                                        | Done                     | A dry run that counts rows per secret type.                                                                                                                                                                                                                                                              |
| G3   | Audit log retention                                                           | Done for self-hosting    | Export before rows expire. Show the oldest kept record on the activity page.                                                                                                                                                                                                                             |
| G4   | More than one control plane                                                   | Partial                  | Polling, rate limits, and poll back-off shared through PostgreSQL. Worker leases and takeover. Tests that kill a process mid-run.                                                                                                                                                                        |
| G5   | Servers that connect out to esa                                               | Not started              | Only if early customers need it.                                                                                                                                                                                                                                                                         |
| G6   | RHEL-family servers (dnf)                                                     | Not started              | Only if early customers need it. Start with one narrow, tested release.                                                                                                                                                                                                                                  |

## Order

1. **C0 and G1** (about 3 to 5 weeks). Most later work needs evidence with an age and helpers that say what they support.
2. **Finish F1** (about 3 to 4 weeks), then **F3** (about 3 weeks).
3. **Close the F4 and F5 gaps** (about 3 to 4 weeks). Enforcing the approved package list in the helper comes first; it is the largest safety gap left in plans.
4. **F6 scanner choice** (1 to 2 weeks to test, then build), **F7**, and the **F2 probes** once the decision below is made.
5. **G4**, then **G5** and **G6** as customers need them. Running more than one control plane needs G4 first.

## Open decisions

These change scope, so they are settled before the work starts.

| Decision                                                                    | Blocks                                     |
| --------------------------------------------------------------------------- | ------------------------------------------ |
| Who the first customers are: fleet size, distros, Docker and network setup  | G5, G6, the size of F3                     |
| Whether esa runs external probes, or only supports probes an operator hosts | F2 probes                                  |
| How long notices, reports, and the audit log are kept on the hosted service | G3 on the hosted service, F1 to F3 storage |
| Whether plans can require two approvers, and what emergency override exists | F5                                         |
| Whether owners can silence critical notices, and for how long               | F1                                         |
| Where backup support stops: evidence only, or isolated restore tests too    | F7                                         |
| How long old helper versions are supported                                  | G1                                         |

## Not planned

A general firewall solver, an exploit scanner, a backup engine, key revocation on servers, a directory or HR sync, and any claim of complete discovery, compliance, or "secure".
