---
title: License
description: AGPL-3.0-only in plain words, trademarks, the Developer Certificate of Origin, and the warranty.
---

Copyright (C) 2026 Niraj Dilshan and contributors.

The license is the GNU Affero General Public License, version 3 only. The SPDX identifier is `AGPL-3.0-only`. It is not "or any later version". The full text is `LICENSE` in the source tree.

Easy Sys Admin will remain available under AGPL-3.0-only. We do not use a CLA and cannot relicense contributors' code.

## Can I self-host it?

Yes. You can run it for any use, including commercial use. There is no license fee, and the license sets no usage cap. The public self-hosted release is still marked coming soon. The license terms are already these.

## What does the AGPL ask for?

You can use, study, and change the software, and you can give copies to other people. If you give someone a copy, you give them the same license and you make the source available under these terms.

The extra AGPL rule is about a network. If you modify the software and let other people use that modified version over a network, you must offer them the source of the version they are talking to. The web footer has a Source code link for that. If you ship your own changes, set `ESA_SOURCE_URL` to the place where that source can be downloaded. See [Self-hosting](/self-hosting).

Running the upstream source without your own changes still ships with that link, pointed at the upstream repository and the commit that is running.

Managed Cloud, when it exists, is our hosted service. It is not a separate license. Offering a modified copy as your own hosted service is allowed under the AGPL, and the network rule above applies.

## Trademarks

The Easy Sys Admin name and logo belong to Niraj Dilshan (infinit3HQ). They are not covered by the license. A fork must use its own name and logo. You may say, truthfully, that your program is based on Easy Sys Admin or compatible with it. Managed Cloud is reserved. Details are in `TRADEMARKS.md` in the source tree.

## Warranty

The software is provided as is, without warranty of any kind, to the extent the license allows. The authors are not liable for damage from using it. Do not point it at a machine you cannot restore.

## Contributing

There is no contributor license agreement. A contribution is certified with the Developer Certificate of Origin, version 1.1. The text is the file `DCO` in the source tree, copied from [developercertificate.org](https://developercertificate.org/).

`git commit -s` adds a `Signed-off-by` line for the commit author. That is the certification. Pull requests are checked for that line. How to set up a development copy, and how to sign commits that are already made, is in `CONTRIBUTING.md`.

If you include someone else's work, or code a tool wrote, say so in the pull request. You still have to have the right to submit it.

Report security problems in private, as described in [Security](/security).
