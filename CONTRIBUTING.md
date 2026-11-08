# Contributing

Contributions are certified with the [Developer Certificate of Origin](DCO), version 1.1, not a contributor license agreement. Adding a `Signed-off-by` trailer is how you certify that. The text is the one published at [developercertificate.org](https://developercertificate.org/).

Easy Sys Admin will remain available under AGPL-3.0-only. We do not use a CLA and cannot relicense contributors' code.

1. Set up the repository as described in [docs/development.md](docs/development.md).
2. Make the change. API changes start in `packages/api/openapi.yaml`; run `pnpm generate` after editing it.
3. Run the full check and make sure `git status` shows no unexpected generated changes:

   ```sh
   pnpm turbo run generate lint typecheck build test
   ```

4. Check UI changes in a browser at desktop and phone widths.
5. Update the docs that describe what you changed. The table at the end of [AGENTS.md](AGENTS.md) says which page covers what.
6. Commit with a sign-off, and open a pull request against `main`. CI must pass. Merges to `main` deploy.

## Sign-off

`git commit -s` adds a trailer from your Git author name and email:

```text
Signed-off-by: Your Name <you@example.com>
```

That name and email must be the commit's author. The DCO check on a pull request compares them. Set `user.name` and `user.email` before you commit if they are wrong.

```sh
git commit -s -m "Add ESA_ALLOW_PRIVATE_NETWORKS setting"
```

A commit that is already made can be signed in place:

```sh
git commit --amend -s
```

A branch with several unsigned commits can be signed in one pass. This rewrites those commits:

```sh
git rebase --signoff origin/main
```

Push the rewritten branch. Every commit on the pull request needs its own trailer, including a merge commit. `git commit -s` when you finish a merge adds it.

The CI workflow `.github/workflows/dco.yml` does this check. The [DCO GitHub App](https://github.com/apps/dco) is an alternative that comments on the pull request instead of a workflow.

## What you are certifying

Read [DCO](DCO) before you sign off. In short, you certify that you can submit the change under AGPL-3.0-only.

Say so in the pull request when any of this is true:

- Part of the change is someone else's work. Name the source and the license, and keep that license notice with the file.
- A tool wrote part of the change, including an AI tool. You still have to review it, and you still have to have the right to submit it. Do not sign off on output that copies code you are not allowed to license.

Commit subjects are short and imperative, for example `Add ESA_ALLOW_PRIVATE_NETWORKS setting`. Changes that only touch the web app start with `web:`. The body says why.

Report security problems privately, as described in [SECURITY.md](SECURITY.md).
