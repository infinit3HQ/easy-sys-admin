# Easy Sys Admin Privacy Policy

_Last updated: [TODO: date]_ · **DRAFT: pending lawyer review. Not legal advice.**

This policy explains what personal data the hosted Easy Sys Admin service at easysysadmin.app (the "Service") collects, why, and what choices you have. The Service is run by **Niraj Dilshan, an individual doing business as infinit3HQ**, Sri Lanka [TODO: address] ("we", "us"), who is responsible for your account data. Contact: support@easysysadmin.app.

This policy covers only the hosted Service. If you self-host the open source version, we don't receive any of your data.

## 1. What We Collect

**Account data:** Your name and email address, plus your Google account ID if you sign in with Google. We do not store a profile picture. We also keep your sign-in history and the names of your spaces and team members.

**Server and operational data ("Customer Data"):**

- Information about servers you connect: hostname, IP address, operating system, installed packages, services and containers, and status and metrics.
- **Logs and command output** that the Service reads from or produces on your servers.
- An audit trail of actions taken through the Service, recording who did what, when, and on which server.

Also stored: the pinned SSH host key and its fingerprint; whether a reboot is required; pending package lists; group schedules; alert rules and alert events; TLS certificate checks (hostname, port, expiry, issuer); and run history. A run stores its kind, status, summary, and, for an update, the package list and the last 40 lines of that upgrade's log. Journal and container logs shown in the board are read when you ask for them and are not saved as a log archive.

**Credentials:** The SSH key material and connection details the Service needs to reach your servers through the `esa` user, plus API and MCP tokens. Each space has one ed25519 SSH key pair. The private key is encrypted at rest with AES-256-GCM under ESA_MASTER_KEY and stored in the database. ESA_MASTER_KEY is 32 random bytes and lives only in the control plane's environment, not in the database. Each ciphertext is bound to its row, so a copied value does not decrypt in another row. A sudo login is stored only if an owner chooses to keep it, and it is encrypted the same way. The API does not return a stored private key or password. API tokens and the session cookie are stored only as SHA-256 hashes.

**Usage and device data:** IP address, browser and device type, pages and features used, CLI, API, and MCP request metadata, and error reports.

**Support communications:** Messages you send us.

**Cookies:** We use essential cookies for sign-in and security. The session cookie is `__Host-esa_session` when the site is https, otherwise `esa_session`. It is HttpOnly and SameSite=Lax, and it ends after 24 hours without use or 7 days after sign-in, whichever comes first. A short-lived cookie carries the OpenID login state and is cleared when sign-in finishes. There are no analytics cookies, and the application does not ask for cookie consent.

**Note on logs:** Server logs can contain other people's personal data, such as IP addresses or usernames of your own users. For that data, you decide what's collected, and we process it on your behalf. Please don't connect systems whose logs you aren't permitted to share. [LAWYER: controller/processor roles, and a DPA for Managed Cloud.]

## 2. How We Use It

- **To provide the Service:** Signing you in, connecting to your servers, running the actions you request, and showing results. [Legal basis: contract]
- **To keep things secure:** Detecting abuse, investigating incidents, and enforcing our Terms. [Legal basis: legitimate interests and legal obligation]
- **To improve the Service:** Fixing bugs and understanding feature usage, using aggregated or minimal data where possible. [Legal basis: legitimate interests]
- **To communicate:** Service, security, and beta notices, and replies to support requests. Marketing emails go out only with your consent, and you can unsubscribe anytime. [Legal basis: contract and consent]
- **To meet legal obligations.**

We **don't sell** your personal data. We don't use Customer Data to train AI models. The Service does not send Customer Data to an AI provider. The code does not call an AI API. The MCP endpoint is a server your own client connects to. Data that client requests is sent to that client, under your token. If you connect your own AI assistant through the MCP endpoint, the data it requests goes to that assistant under your control and its provider's terms.

## 3. Who We Share It With

We share data only with service providers ("subprocessors") who help us run the Service, under contracts that protect it.

| Subprocessor | Purpose | Location |
|---|---|---|
| [TODO: hosting provider] | Hosting the Service and data. The source does not name a vendor. | [TODO] |
| Google (accounts.google.com) | Sign-in, when Google sign-in is turned on | [TODO] |
| OpenID Connect issuer (ESA_OIDC_ISSUER) | Sign-in, only when an issuer is configured. The source does not name a vendor. | [TODO] |
| SMTP host (ESA_SMTP_HOST) | Confirmation, password reset, and service mail, only when a host is set. The source does not name a vendor. | [TODO] |
| Alert destinations you configure (ntfy, a webhook, Discord, Slack, or email) | Delivery of alerts you turn on, to the address you enter | The address you set |
| None for error tracking or analytics | The code does not call an error tracker or an analytics service |  |

We may also disclose data if the law requires it, to protect rights, safety, or the Service, or as part of moving the Service into a company or selling it. We'll tell you if a transfer happens. We'll update this list before adding new subprocessors [TODO: and notify you of changes].

## 4. International Transfers

We're based in Sri Lanka, and our providers may process data in other countries [TODO: list]. Where required, we use appropriate safeguards such as standard contractual clauses. [LAWYER]

## 5. How Long We Keep It

| Data | Kept for |
|---|---|
| Account data | While your account is active |
| Logs and command output | Journal and container logs are not stored. The last 40 lines of an upgrade's log are kept on that run until the server or the space is deleted. |
| Audit trail | Kept until the space is deleted. The code does not delete old entries on a timer. |
| Credentials | Until you remove the server or delete your account |
| Usage data and error reports | Not stored as a product record. There is no retention period in the code. |

When you delete your account, we delete your data within [TODO: 30] days and remove it from backups within a further [TODO: 35] days. The exceptions are anything we must keep by law, and minimal records needed for security or disputes. Remember to remove the `esa` user, the `esa-helper` component, and the authorized key from your servers too.

## 6. Security

We protect data with HTTPS for the hosted site (an enroll command is refused on plain http except on localhost), AES-256-GCM for secrets at rest under ESA_MASTER_KEY, hashed session cookies and API tokens, an SSH login that can run only the helper, and an audit log of web, API, and MCP actions. No method is 100% secure. If a breach affects your personal data, we'll notify you, and the relevant authorities where required, without undue delay [TODO: within 72 hours where required].

## 7. Your Rights

Depending on where you live, including under Sri Lanka's Personal Data Protection Act No. 9 of 2022 and, where applicable, the EU and UK GDPR [LAWYER: confirm applicable laws], you may have the right to:

- access, correct, or delete your personal data;
- export your data;
- object to or restrict certain processing; and
- withdraw consent at any time.

Email support@easysysadmin.app, and we'll respond within [TODO: 30] days. You may also complain to your local data protection authority.

## 8. Children

The Service isn't intended for anyone under 18, and we don't knowingly collect their data.

## 9. Changes

We'll post updates here and change the date above. For material changes, we'll notify you by email or in the Service before they take effect.

## 10. Contact

Niraj Dilshan (infinit3HQ) · [TODO: address] · support@easysysadmin.app
