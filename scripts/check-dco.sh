#!/bin/sh
# Fail unless every commit in BASE..HEAD has a Signed-off-by trailer for its author.
# BASE defaults to the GitHub pull request base. Locally, pass the base sha as $1.
set -eu

base=${1:-${BASE:-}}
if [ -z "$base" ]; then
  echo "usage: BASE=<sha> $0   or   $0 <base-sha>" >&2
  exit 2
fi

missing=0
# rev-list is newline-separated. A while-read from a pipe would drop the exit flag.
commits=$(git rev-list --reverse "$base"..HEAD)
for sha in $commits; do
  author=$(git log -1 --format='%an <%ae>' "$sha")
  body=$(git log -1 --format='%B' "$sha")
  if printf '%s\n' "$body" | git interpret-trailers --parse | grep -Fqx "Signed-off-by: $author"; then
    continue
  fi
  echo "Commit $sha by $author has no Signed-off-by trailer for that author." >&2
  echo "Add one with git commit -s, or git rebase --signoff, then push again." >&2
  missing=1
done

exit "$missing"
