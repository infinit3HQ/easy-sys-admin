# Contributing

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
