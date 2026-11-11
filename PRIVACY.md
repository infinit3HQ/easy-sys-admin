# Easy Sys Admin Privacy Policy

_Last updated: November 11, 2026_

This policy explains what personal data the hosted Easy Sys Admin service at easysysadmin.app (the "Service") collects, why, and what choices you have. The Service is run by **Niraj Dilshan, doing business as infinit3HQ**, Colombo, Sri Lanka ("we", "us"), who is responsible for your account data. Contact: support@easysysadmin.app.

This policy covers only the hosted Service. If you self-host the open source version, we don't receive any of your data.

## 1. What We Collect

**Account data:** Your name and email address, plus your Google account ID if you sign in with Google. We do not store a profile picture. We also keep your sign-in history and the names of your spaces and team members.

**Server and operational data ("Customer Data"):**

- Information about servers you connect: hostname, IP address, operating system, installed packages, services and containers, and status and metrics. A short note you write on a server is stored with that server and is not sent to the machine.
- **Logs and command output** that the Service reads from or produces on your servers.
- An audit trail of actions taken through the Service, recording who did what, when, and on which server.

Also stored: the pinned SSH host key and its fingerprint; whether a reboot is required; pending package lists; group schedules; alert rules and alert events; TLS certificate checks (hostname, port, expiry, issuer); and run history. A run stores its kind, status, summary, and, for an update, the package list and the last 40 lines of that upgrade's log. About once an hour the board stores an exposure summary for each server: public listen ports, SSH login settings, the firewall name, and counts of extra root accounts and unrestricted sudo rules. That summary does not include password hashes, SSH key material, the user list, or login addresses. Those are shown when you open the access report and are not saved. Journal and container logs shown in the board are read when you ask for them and are not saved as a log archive.

**Credentials:** The SSH key material and connection details the Service needs to reach your servers through the `esa` user, plus API and MCP tokens. Each space has one ed25519 SSH key pair. The private key is encrypted at rest with AES-256-GCM under ESA_MASTER_KEY and stored in the database. ESA_MASTER_KEY is 32 random bytes and lives only in the control plane's environment, not in the database. Each ciphertext is bound to its row, so a copied value does not decrypt in another row. A sudo login is stored only if an owner chooses to keep it, and it is encrypted the same way. The API does not return a stored private key or password. API tokens and the session cookie are stored only as SHA-256 hashes.

**Usage and device data:** IP address, browser and device type, pages and features used, CLI, API, and MCP request metadata, and error reports.

**Support communications:** Messages you send us.

**Cookies:** We use essential cookies for sign-in and security. The session cookie is `__Host-esa_session` when the site is https, otherwise `esa_session`. It is HttpOnly and SameSite=Lax, and it ends after 24 hours without use or 7 days after sign-in, whichever comes first. A short-lived cookie carries the OpenID login state and is cleared when sign-in finishes. There are no analytics cookies, and the application does not ask for cookie consent.

**Note on logs:** Server logs can contain other people's personal data, such as IP addresses or usernames of your own users. For that data, you decide what's collected, and we process it on your behalf. Please don't connect systems whose logs you aren't permitted to share.

## 2. How We Use It

- **To provide the Service:** Signing you in, connecting to your servers, running the actions you request, and showing results. (Legal basis: contract performance)
- **To keep things secure:** Detecting abuse, investigating incidents, and enforcing our Terms. (Legal basis: legitimate interests and security compliance)
- **To improve the Service:** Fixing bugs and understanding feature usage, using aggregated or minimal data where possible. (Legal basis: legitimate interests)
- **To communicate:** Service, security, and beta notices, and replies to support requests. Marketing emails go out only with your consent, and you can unsubscribe anytime. (Legal basis: contract and consent)
- **To meet legal obligations.**

We **don't sell** your personal data. We don't use Customer Data to train AI models. The Service does not send Customer Data to an AI provider. The code does not call an AI API. The MCP endpoint is a server your own client connects to. Data that client requests is sent to that client, under your token. If you connect your own AI assistant through the MCP endpoint, the data it requests goes to that assistant under your control and its provider's terms.

## 3. Who We Share It With

We share data only with infrastructure and service providers ("subprocessors") that help us operate the Service, under contracts that protect it.

| Subprocessor | Purpose | Location |
|---|---|---|
| Oracle Cloud Infrastructure (Oracle America, Inc.) | Hosting the Service infrastructure and PostgreSQL database | Global / Cloud Data Centers |
| Google (accounts.google.com) | User authentication and sign-in (when enabled) | Global |
| OpenID Connect Provider | Sign-in authentication (only when an external OIDC provider is configured) | Host-specific |
| SMTP Provider | Transactional emails (confirmation, password resets) | Host-specific |
| Alert destinations you configure (ntfy, webhooks, Discord, Slack, or email) | Delivery of notification alerts you configure | The endpoint or address you set |
| None for error tracking or analytics | The codebase does not call third-party error trackers or analytics trackers | N/A |

We may also disclose data if required by law, to protect life, safety, or legal rights, or as part of a corporate reorganization or transfer. We will notify you before any material structural change takes effect.

## 4. International Transfers

We are based in Sri Lanka, and our cloud infrastructure providers process data across secure, compliant data centers globally. Where required by applicable law, we apply appropriate technical safeguards such as Standard Contractual Clauses (SCCs) and strong end-to-end encryption.

## 5. How Long We Keep It

| Data | Kept for |
|---|---|
| Account data | While your account remains active |
| Logs and command output | Journal and container logs are not permanently archived. The last 40 lines of an upgrade run's log are retained on that run until the server or space is deleted. |
| Audit trail | Retained until the space is deleted. |
| Credentials | Until you remove the server or delete your account |
| Usage data and error reports | Kept in temporary operational memory; no indefinite tracking logs |

When you delete your account, we delete your data within 30 days and purge it from encrypted backups within a further 35 days. The exceptions are records strictly required by law or necessary for dispute resolution and security defense. Remember to remove the `esa` user, the `esa-helper` binary, and the authorized SSH key from your managed servers as well.

## 6. Security

We protect data with HTTPS for the hosted site (an enroll command is refused over plain HTTP except on localhost), AES-256-GCM encryption for secrets at rest under ESA_MASTER_KEY, hashed session cookies and API tokens, an SSH login that is strictly restricted to running the helper binary, and an immutable audit trail of web, API, and MCP actions. No method is 100% impenetrable. If a security incident affects your personal data, we will notify you and relevant supervisory authorities without undue delay and within 72 hours where required by applicable regulations.

## 7. Your Rights

Depending on where you live, including under Sri Lanka's Personal Data Protection Act No. 9 of 2022 and, where applicable, the EU and UK GDPR, you have the right to:

- Access, correct, or delete your personal data;
- Export your account and server inventory data;
- Object to or restrict certain processing; and
- Withdraw consent at any time.

Email support@easysysadmin.app, and we will respond within 30 days. You may also lodge a complaint with your local data protection supervisory authority.

## 8. Children

The Service is not intended for anyone under 18, and we do not knowingly collect personal data from minors.

## 9. Changes

We will post any updates here with an updated revision date. For material changes, we will notify you by email or through the Service dashboard before changes take effect.

## 10. Contact

Niraj Dilshan (infinit3HQ) · Colombo, Sri Lanka · support@easysysadmin.app
