# Contributing

Outside contributions need a signed Contributor License Agreement before they can be merged. The text is [CLA.md](CLA.md). It is a draft pending legal review. It is a license grant in the style of the Apache individual CLA. You keep the copyright. It is not an assignment. The grant includes the right to sublicense and relicense, and the project stays available under AGPL-3.0-only.

CLA Assistant posts a comment on the pull request. To sign, reply with this exact sentence:

```
I have read the CLA Document and I hereby sign the CLA
```

You only sign once. The check stays red until every contributor on the pull request has signed, or is on the allowlist (dependabot, renovate, and github-actions). Signatures are stored on the `cla-signatures` branch. Bots cannot sign, which is why they are allowlisted.

1. Set up the repository as described in [docs/development.md](docs/development.md).
2. Make the change. API changes start in `packages/api/openapi.yaml`; run `pnpm generate` after editing it.
3. Run the full check and make sure `git status` shows no unexpected generated changes:

   ```sh
   pnpm turbo run generate lint typecheck build test
   ```

4. Check UI changes in a browser at desktop and phone widths.
5. Update the docs that describe what you changed. The table at the end of [AGENTS.md](AGENTS.md) says which page covers what.
6. Open a pull request against `main`. CI must pass. Merges to `main` deploy.

Commit subjects are short and imperative, for example `Add ESA_ALLOW_PRIVATE_NETWORKS setting`. Changes that only touch the web app start with `web:`. The body says why.

Report security problems privately, as described in [SECURITY.md](SECURITY.md).
