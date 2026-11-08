# Easy Sys Admin Terms of Service

_Last updated: November 8, 2026_

These Terms are an agreement between you and **Niraj Dilshan, doing business as infinit3HQ** ("we", "us"), Colombo, Sri Lanka. They cover your use of the hosted Easy Sys Admin service at easysysadmin.app, including its web app, CLI access, API, and MCP endpoint (the "Service").

By creating an account or using the Service, you agree to these Terms. If you use the Service for an organization, you confirm you're authorized to accept these Terms for it, and "you" includes that organization.

**The short version:** You give us restricted access to servers you control so the Service can manage them. You stay responsible for those servers, your backups, and approving changes. The Service is a free beta, provided as-is, and our liability is strictly limited.

## 1. Beta Service

The Service is currently a **free public beta**. Features, limits, and availability may change, break, or be updated at any time. There is no formal service level agreement (SLA), uptime commitment, or 24/7 support obligation during the beta. Do not rely on the Service as your only way to access or recover your servers.

## 2. Accounts

- You must be at least 18 years of age and able to form a legally binding contract.
- You sign in with Google or by email. Keep your account and any API or MCP tokens secure. You are responsible for all activity under your account.
- Notify us immediately at support@easysysadmin.app if you suspect unauthorized access.
- Provide accurate information. One person or organization may not create multiple accounts to circumvent beta limits.

## 3. How the Service Accesses Your Servers

To manage a server, you install a restricted `esa` user and the `esa-helper` component on it and authorize the Service to connect over SSH. Through them, the Service can, at your request or as you configure it:

- update and install packages;
- restart services and containers;
- reboot the server; and
- read system and application logs.

It can also read live status (CPU, memory, disks and inodes, load, network, time sync, failed units, and Docker), list pending packages, clean idle Docker build cache, old VS Code server copies, and the journal, start or stop a service, pull and recreate one Compose service, and read who can log in, which ports are listening, and which jobs are scheduled. Cleanups do not delete Docker volumes or images a container is actively using, and they do not run system prune. Critical units such as sshd and Docker cannot be stopped from the Service.

**You authorize these actions** for every server you connect. You can revoke access at any time by removing the `esa` user, the `esa-helper` component, and the authorized SSH key from the server, or by removing the server from the Service dashboard.

## 4. Your Responsibilities

You are responsible for:

- **Authority:** Connecting only servers that you own or are legally authorized to manage, with all necessary permissions from your employer, clients, or hosting provider.
- **Backups:** Maintaining current, verified backups of your servers and data **before** executing operations through the Service. Updates, restarts, and reboots carry inherent operational risk.
- **Approving changes:** Reviewing and approving changes before they run, especially on production systems. Any action triggered from your account, including through the CLI, API, MCP endpoint, or an AI agent you connect to it, is legally attributable to you.
- **AI agents and automations:** If you connect an AI assistant or automation through the MCP endpoint or API, you are responsible for what it executes with your access.
- **Compliance:** Complying with all applicable laws and contracts governing your servers and data.

## 5. Acceptable Use

You may not use the Service to:

- access or manage systems you are not authorized to manage;
- violate applicable laws or third-party rights;
- distribute malware, operate botnets, mine cryptocurrency on systems you do not own, send spam, or port-scan external networks;
- probe, stress-test, reverse-engineer, or bypass the Service's security controls, limits, or authentication, except as permitted under our security disclosure policy;
- resell or white-label the hosted Service to third parties without prior written consent; or
- disrupt the stability or performance of the Service for other users.

Nothing here limits your rights under the AGPL-3.0 license for the open-source software (see Section 9).

## 6. Usage Limits

During the beta, each space may connect up to **10 servers**, and each account up to **20 servers in total**. We may adjust limits or apply fair-use throttles on API and MCP requests as needed.

## 7. Your Data

- **You own your data:** This includes server information, logs, command output, and configuration you bring into the Service ("Customer Data").
- **License to operate:** You grant us a limited, worldwide license to host, process, transmit, and display Customer Data strictly as required to provide, secure, and operate the Service, per our [Privacy Policy](PRIVACY.md).
- **Third-Party Personal Data:** If Customer Data contains personal data of your end-users, you represent that you have a lawful basis to process and share it.
- **Feedback:** If you submit feedback or product suggestions, we may incorporate them without financial obligation or restriction.

## 8. Security and Incident Notification

We implement industry-standard safeguards to protect the Service and credentials. The space's SSH private key, sudo credentials you choose to retain, and notification tokens are encrypted at rest with AES-256-GCM under ESA_MASTER_KEY. The key is 32 random bytes stored in the control plane's secure environment, never in the database. API tokens and session cookies are stored exclusively as cryptographic SHA-256 hashes. The `esa` SSH user is restricted to executing only the verified helper binary without interactive shell access.

If we confirm a security incident that impacts your account or Customer Data, we will:

- notify you without undue delay, and within 72 hours where required by applicable data protection regulations, by email to your account address;
- take immediate corrective measures to contain and mitigate the incident; and
- provide reasonable operational details to assist in your incident response.

## 9. Open Source Version Is Separate

Easy Sys Admin's underlying software is released under the **GNU Affero General Public License v3.0 only (AGPL-3.0-only)**. If you self-host the open-source version, the AGPL license governs your use, and these hosted Terms do not apply. These Terms apply solely to the hosted cloud service at easysysadmin.app.

The Easy Sys Admin trademark, name, and visual assets are not licensed under the AGPL.

## 10. Third-Party Services and Packages

The Service interacts with third-party providers (such as Google OAuth, Oracle Cloud Infrastructure, and SMTP relays) as detailed in the Privacy Policy. Your servers' OS distributions and software repositories are third-party systems. We do not manufacture or guarantee third-party software packages or upstream distribution repositories.

## 11. Fees and Paid Plans

The beta is free of charge. If and when commercial tiers (such as Managed Cloud) are introduced, we will provide at least 30 days' advance notice before applying charges, allowing you ample time to export data or transition to self-hosting.

## 12. Suspension and Termination

- **By you:** You may discontinue use at any time. Organization owners can delete an organization space, which purges the associated operational data.
- **By us:** We may suspend or terminate account access or server polling if you violate these Terms, introduce security risks, or if the beta is retired. Where feasible, advance notice will be provided.
- **Survival:** Sections 4, 5, 7, 9, and 14–19 survive termination.

## 13. Data Retention and Account Deletion

When an account or server connection is removed, active polling ceases immediately. Customer Data and encrypted credentials are purged within 30 days and cleared from backup archives within an additional 35 days, subject to statutory retention requirements.

## 14. Disclaimer of Warranties

**To the maximum extent permitted by applicable law, the Service is provided "AS IS" and "AS AVAILABLE", without warranties of any kind.** We disclaim all warranties, express, implied, or statutory, including warranties of merchantability, fitness for a particular purpose, uptime, security, and non-infringement. We do not warrant that server updates or operations initiated through the Service will be error-free or uninterrupted. You operate the Service on your infrastructure at your sole risk.

## 15. Limitation of Liability

**To the maximum extent permitted by applicable law:**

(a) We will not be liable for any indirect, incidental, special, consequential, or punitive damages, or for loss of profits, revenue, data, business interruption, or server downtime, under any legal theory.

(b) Our aggregate liability for all claims arising from or related to the Service will not exceed the greater of: (i) the total amount paid by you to us for the Service in the 12 months preceding the claim, or (ii) **USD $50.00**. During the free beta period, liability under clause (i) is zero.

## 16. Indemnification

You agree to defend, indemnify, and hold harmless Niraj Dilshan, infinit3HQ, and our affiliates from any claims, liabilities, damages, and legal costs arising from: (a) your use of the Service; (b) systems or servers you connect without authorization; (c) your violation of these Terms; or (d) actions executed by AI agents or automations authorized under your account.

## 17. Governing Law and Dispute Resolution

These Terms are governed by the laws of Sri Lanka, without regard to conflict-of-law principles. Any legal dispute arising under these Terms shall be resolved in the competent courts of Colombo, Sri Lanka. Before initiating formal legal proceedings, parties agree to attempt good-faith informal resolution by contacting support@easysysadmin.app for at least 30 days.

## 18. Changes to Terms

We may revise these Terms periodically. For material modifications, we will provide at least 14 days' advance notice via email or dashboard notification before changes become effective. Continued use of the Service after the effective date constitutes acceptance.

## 19. General

- **Entire Agreement:** These Terms and the Privacy Policy constitute the entire agreement between the parties regarding the Service.
- **Severability:** If any provision is deemed unenforceable, the remaining provisions remain in full force.
- **No Waiver:** Failure to enforce a provision does not constitute a waiver of future enforcement.
- **Assignment:** You may not assign these Terms without prior written approval. We may assign our rights in connection with a corporate reorganization, merger, or acquisition.

## 20. Contact

Niraj Dilshan (infinit3HQ) · Colombo, Sri Lanka · support@easysysadmin.app
