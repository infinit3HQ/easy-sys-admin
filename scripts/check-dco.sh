#!/bin/sh
# Fail unless every non-merge commit in BASE..HEAD has a Signed-off-by trailer
# for its author, or for the author's entry in .mailmap. Merge commits are skipped: a merge of the base branch does
# not certify new work, and GitHub adds its own merge commit while testing a
# pull request. That commit has no Signed-off-by line.
# BASE defaults to the GitHub pull request base. Locally, pass the base sha as $1.
set -eu

base=${1:-${BASE:-}}
if [ -z "$base" ]; then
  echo "usage: BASE=<sha> $0   or   $0 <base-sha>" >&2
  exit 2
fi

missing=0
# rev-list is newline-separated. A while-read from a pipe would drop the exit flag.
commits=$(git rev-list --no-merges --reverse "$base"..HEAD)
for sha in $commits; do
  author=$(git log -1 --format='%an <%ae>' "$sha")
  # The same author after .mailmap, which lists other addresses of one person,
  # such as the noreply address GitHub uses for web merges.
  mapped=$(git log -1 --format='%aN <%aE>' "$sha")
  trailers=$(git log -1 --format='%B' "$sha" | git interpret-trailers --parse)
  if printf '%s\n' "$trailers" | grep -Fqx -e "Signed-off-by: $author" -e "Signed-off-by: $mapped"; then
    continue
  fi
  echo "Commit $sha by $author has no Signed-off-by trailer for that author." >&2
  echo "Add one with git commit -s, or git rebase --signoff, then push again." >&2
  missing=1
done

exit "$missing"
