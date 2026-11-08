# Easy Sys Admin Terms of Service

_Last updated: [TODO: date]_ · **DRAFT: pending lawyer review. Not legal advice.**

These Terms are an agreement between you and **Niraj Dilshan, an individual doing business as infinit3HQ** ("we", "us"), located in Sri Lanka [TODO: business address]. They cover your use of the hosted Easy Sys Admin service at easysysadmin.app, including its web app, CLI access, API, and MCP endpoint (the "Service").

By creating an account or using the Service, you agree to these Terms. If you use the Service for an organization, you confirm you're authorized to accept these Terms for it, and "you" includes that organization.

**The short version:** You give us restricted access to servers you control so the Service can manage them. You stay responsible for those servers, your backups, and approving changes. The Service is a free beta, provided as-is, and our liability is very limited.

## 1. Beta Service

The Service is currently a **free public beta**. Features, limits, and availability may change, break, or be removed at any time, with or without notice. There's no service level agreement, uptime commitment, or support obligation during the beta. Don't rely on the Service as your only way to access or recover your servers.

## 2. Accounts

- You must be at least 18 and able to form a binding contract.
- You sign in with Google or by email. Keep your account and any API or MCP tokens secure. You're responsible for all activity under your account.
- Tell us immediately at support@easysysadmin.app if you suspect unauthorized access.
- Give us accurate information. One person or organization may not create multiple accounts to get around limits.

## 3. How the Service Accesses Your Servers

To manage a server, you install a restricted `esa` user and the `esa-helper` component on it and authorize the Service to connect over SSH. Through them, the Service can, at your request or as you configure it:

- update and install packages;
- restart services and containers;
- reboot the server; and
- read system and application logs.

It can also read live status (CPU, memory, disks and inodes, load, network, time sync, failed units, and Docker), list pending packages, clean idle Docker build cache, old VS Code server copies, and the journal, start or stop a service, pull and recreate one Compose service, and read who can log in, which ports are listening, and which jobs are scheduled. Cleanups do not delete Docker volumes or images a container is using, and they do not run system prune. Units such as ssh and Docker cannot be stopped from the Service.

**You authorize these actions** for every server you connect. You can revoke access at any time by removing the `esa` user, the `esa-helper` component, and the authorized SSH key from the server, or by removing the server from the Service. In the board, delete the server with "Also remove esa from the server" on (it is on by default). That runs `esa-helper uninstall`, which removes the sudo rule, the helper at `/usr/local/lib/esa`, and the `esa` user when that user still has the comment and home the installer created. If uninstall fails, the server stays in the Service until you fix the connection or delete it without uninstalling. You can do the same removal as root on the server. The steps are at https://docs.easysysadmin.app/remove-a-server.

## 4. Your Responsibilities

You're responsible for:

- **Authority:** You only connect servers that you own or are authorized to manage, and you have every permission needed, including from your employer, clients, or hosting provider.
- **Backups:** Keep current, tested backups of your servers and data **before** using the Service. Updates, restarts, and reboots can cause downtime or data loss.
- **Approving changes:** Review and approve changes before they run, especially on production systems. Test updates in a non-production environment where possible. Any action triggered from your account, including through the CLI, the API, the MCP endpoint, or an AI agent you connect to it, counts as your action.
- **AI agents and automations:** If you connect an AI assistant or automation through the MCP endpoint or API, you're responsible for what it does with your access.
- **Your systems:** Keep your servers patched and secure, and handle the security of your own network and accounts.
- **Compliance:** Follow the laws and contracts that apply to your servers and the data on them, including data protection laws for any personal data in your logs.

## 5. Acceptable Use

You may not use the Service to:

- access or manage systems you're not authorized to manage;
- break the law or infringe anyone's rights;
- distribute malware, run botnets, mine cryptocurrency on systems you don't own, send spam, or attack or scan other systems without permission;
- probe, overload, reverse-engineer, or bypass the Service's security, limits, or access controls, except as allowed by our security policy (https://docs.easysysadmin.app/security);
- resell or provide the hosted Service to third parties without our written permission; or
- interfere with other users.

Nothing here limits your rights under the AGPL-3.0 license for the open source code (see Section 9).

## 6. Usage Limits

During the beta, each space may connect up to **10 servers**, and each account up to **20 servers in total**. We may change limits, or apply fair-use limits on API, MCP, or other usage, at any time.

## 7. Your Data

- **You own your data:** This includes server information, logs, command output, and configuration you bring into the Service ("Customer Data").
- **License to run the Service:** You give us a limited license to host, process, transmit, and display Customer Data only as needed to provide, secure, and improve the Service, and as described in our [Privacy Policy](PRIVACY.md).
- **Personal data of others:** If Customer Data includes other people's personal data (for example in logs), you are responsible for having a lawful basis to share it with us. [TODO: a data processing agreement (DPA) will be offered with the paid Managed Cloud.] [LAWYER]
- **Feedback:** If you send us feedback or ideas, we may use them without any obligation to you.

## 8. Security and Incidents

We use reasonable measures to protect the Service and the credentials it holds. The space's SSH private key, a sudo login an owner chooses to keep, and notification-channel credentials are encrypted at rest with AES-256-GCM. The key is ESA_MASTER_KEY, 32 random bytes kept only in the control plane's environment, not in the database. Each ciphertext is bound to the database row it belongs to. API tokens and the session cookie are stored only as SHA-256 hashes. The API does not return a stored private key or password. The `esa` SSH login can run only the helper, not a shell. Actions from the web app, the API, and MCP are written to an audit log. No system is perfectly secure, though, and we can't guarantee that unauthorized access will never happen.

If we become aware of a security incident that affects your account or Customer Data, we'll:

- notify you without undue delay, and where the law requires within [TODO: 72] hours, by email to your account address;
- take reasonable steps to contain it, which may include suspending connections or rotating credentials; and
- share what we reasonably can to help you respond.

If you suspect a compromise, you should immediately revoke the Service's access on affected servers (Section 3). Report vulnerabilities as described at https://docs.easysysadmin.app/security (a private report, not a public issue). You can also write to support@easysysadmin.app.

## 9. Open Source Version Is Separate

Easy Sys Admin's source code is available under the **GNU Affero General Public License v3.0 only (AGPL-3.0-only)**. If you self-host it, the AGPL governs your use of that software, and these Terms don't apply to it. These Terms govern only the hosted Service we operate. Nothing in these Terms restricts your rights under the AGPL. We provide no support, warranty, or service for self-hosted copies unless we agree otherwise in writing.

The Easy Sys Admin name and logo are **not** licensed under the AGPL. See https://github.com/infinit3HQ/easy-sys-admin/blob/main/TRADEMARKS.md.

## 10. Third-Party Services

The Service relies on third parties such as Google sign-in, hosting, and email delivery (see the Privacy Policy). Your servers' operating systems and package repositories are also third-party. We're not responsible for third-party services, software, or packages, including updates installed from your configured repositories.

## 11. Fees

The beta is free. We may introduce paid plans, including a Managed Cloud offering. We'll give you at least [TODO: 30] days' notice before you're charged for anything, and you can stop using the Service before then. Paid plans may come with additional terms. [TODO: add payment, taxes, refunds, auto-renewal, and SLA terms when paid plans launch.] [LAWYER]

## 12. Suspension and Termination

- **By you:** You can stop using the Service at any time. The product has no control that deletes your user account. An owner can delete an organization by typing its name, which deletes that space and the data it owns. The personal space cannot be deleted.
- **By us:** We may suspend or terminate your access, or any server connection, at any time and for any reason. This includes breaking these Terms, security risks, legal requirements, non-payment (once paid plans exist), or discontinuing the beta or the Service. Where reasonable, we'll give notice first. We may act without notice to protect the Service, other users, or third parties.
- **Ending the beta:** If we discontinue the Service, we'll try to give at least [TODO: 30] days' notice so you can export data and remove access.
- **Survival:** Sections 4, 5, 7 (feedback), 9, and 13–17 survive termination.

## 13. Data Retention and Deletion

When your account is deleted or terminated, we stop connecting to your servers and delete Customer Data and stored credentials within [TODO: 30] days. Data in backups is deleted within a further [TODO: 35] days. The exceptions are anything we must keep by law, and minimal records needed for security, fraud prevention, or resolving disputes. Export anything you need before deleting your account. **You should also remove the `esa` user, the `esa-helper` component, and the authorized key from your servers.** Details are in the [Privacy Policy](PRIVACY.md).

## 14. Disclaimer of Warranties

**To the maximum extent permitted by law, the Service is provided "AS IS" and "AS AVAILABLE", with all faults and without warranties of any kind.** These disclaimed warranties include express, implied, and statutory warranties of merchantability, fitness for a particular purpose, title, non-infringement, accuracy, and uninterrupted or error-free operation. We don't warrant that the Service will be secure, that actions it performs on your servers will succeed or be safe, that updates won't cause outages or data loss, or that any data will be preserved. You use the Service at your own risk.

## 15. Limitation of Liability

**To the maximum extent permitted by law:**

(a) We won't be liable for any indirect, incidental, special, consequential, exemplary, or punitive damages, or for lost profits, revenue, data, or goodwill, business interruption, server downtime, or the cost of substitute services. This applies however caused and under any theory of liability, even if we were told such damages were possible.

(b) Our total liability for all claims relating to the Service or these Terms won't exceed the **greater of (i) the amounts you paid us for the Service in the 12 months before the claim, or (ii) [TODO: USD 50]**. During the free beta, (i) is zero.

(c) These limits apply even if a remedy fails of its essential purpose. They don't limit liability that can't be limited by law, such as for fraud or [LAWYER: confirm under the governing law, e.g. gross negligence or wilful misconduct].

## 16. Indemnity

You'll defend, indemnify, and hold harmless us, Niraj Dilshan, and our contractors from any claims, damages, losses, liabilities, costs, and expenses, including reasonable legal fees, arising from:

- your use of the Service;
- your servers, Customer Data, or any actions taken from your account (including by AI agents or automations you connect);
- your breach of these Terms or the law; or
- your connecting a system you weren't authorized to manage.

## 17. Governing Law and Disputes

These Terms are governed by the laws of **[TODO: Sri Lanka]**, without regard to conflict-of-law rules. Disputes will be resolved exclusively in the courts of **[TODO: Colombo, Sri Lanka]**, unless mandatory law where you live gives you the right to sue elsewhere. [LAWYER: consider arbitration, and how consumer protections apply to users in other countries.] Before filing a claim, please contact us at support@easysysadmin.app so we can try to resolve it informally within 30 days.

## 18. Changes to These Terms

We may update these Terms. For material changes, we'll notify you by email or in the Service at least [TODO: 14] days before they take effect, unless the change is required by law or urgently needed for security. If you keep using the Service after changes take effect, you accept them. If you don't agree, stop using the Service and delete your account.

## 19. General

- **Entire agreement:** These Terms and the Privacy Policy are the entire agreement between us about the Service.
- **Severability:** If any part is unenforceable, the rest stays in effect, and the unenforceable part is enforced as far as possible.
- **No waiver:** Not enforcing a right isn't a waiver of it.
- **Assignment:** You may not transfer these Terms without our consent. We may transfer them, for example to a company we form to operate the Service, or to an acquirer, and we'll tell you if we do.
- **Force majeure:** We're not liable for delays or failures caused by events beyond our reasonable control.
- **Notices:** We'll send notices to your account email. Send yours to support@easysysadmin.app.

## 20. Contact

Niraj Dilshan (infinit3HQ) · [TODO: address] · support@easysysadmin.app
