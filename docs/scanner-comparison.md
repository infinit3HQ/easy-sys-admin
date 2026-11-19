# Scanner comparison

Checked on 17 November 2026 using Grype 0.122.0 and Trivy 0.76.2. Both release archives were downloaded from their official GitHub releases and checked against the published SHA-256 checksums. Their databases were downloaded separately. Scans then used the local caches with updates and external lookups off.

The input is [a synthetic Ubuntu 24.04 SBOM](fixtures/scanner-comparison.cdx.json), made by the actual `buildSBOM` function. Its six deliberately old packages exercise source-package aliases and a versioned kernel package. This is a format comparison, not a measured real fleet or an accuracy benchmark.

| Package                      | Grype package/advisory pairs | Trivy package/advisory pairs |
| ---------------------------- | ---------------------------: | ---------------------------: |
| openssl                      |                           48 |                           43 |
| libssl3t64                   |                           48 |                            0 |
| curl                         |                           45 |                           45 |
| libc6                        |                           33 |                            0 |
| bash                         |                            0 |                            0 |
| linux-image-6.8.0-31-generic |                        10291 |                            0 |
| Total                        |                        10465 |                           88 |

Grype reported an Ubuntu 24.04 distribution and a valid database built at `2026-11-17T06:20:58Z`. Trivy reported Ubuntu 24.04, six OS packages, and a database updated at `2026-11-17T06:59:44Z`. Trivy warned about a third-party SBOM and duplicate OS components. Its documentation [explains the dependence on custom SBOM properties](https://trivy.dev/docs/latest/target/sbom/). A zero in this table is not evidence that a package is safe. Different counts are not proof that either scanner is more accurate.

## Decision

Keep Grype as the supported import and optional automatic scanner. ESA already handles its source-package matches, known-exploited ranking, idle kernels, stale databases, exceptions, and aliases. Replacing it with Trivy on this SBOM would silently change coverage. Do not merge the two result sets or treat a result omitted by one scanner as resolved by the other.

Scanning stays off until an operator imports a report or configures `ESA_SCANNER`. ESA does not bundle or redistribute either scanner or its database. Operators install the scanner separately and download its provider database. The scanner software licences are [Grype Apache-2.0](https://github.com/anchore/grype/blob/main/LICENSE) and [Trivy Apache-2.0](https://github.com/aquasecurity/trivy/blob/main/LICENSE). Those software licences do not replace the terms of individual upstream data feeds. ESA stores identifiers, package versions, fix information, and ranking facts; it does not publish a mirror of advisory databases. This approves the existing optional local-scanner architecture, not redistribution of feed data.

## Repeat the comparison

Download and verify the scanner releases, then explicitly update their local databases. Run these scans with the same fixture and keep their raw reports outside the repository:

```sh
GRYPE_CHECK_FOR_APP_UPDATE=false GRYPE_DB_AUTO_UPDATE=false GRYPE_EXTERNAL_SOURCES_ENABLE=false grype sbom:docs/fixtures/scanner-comparison.cdx.json -o json > /tmp/esa-grype.json
TRIVY_SKIP_VERSION_CHECK=true trivy sbom --offline-scan --skip-db-update --skip-java-db-update --skip-vex-repo-update --format json --output /tmp/esa-trivy.json docs/fixtures/scanner-comparison.cdx.json
node scripts/compare-scanner-reports.mjs /tmp/esa-grype.json /tmp/esa-trivy.json
```

The committed fixture contains no customer server inventory. Raw reports and downloaded databases are not committed. Future database snapshots can produce different counts.
