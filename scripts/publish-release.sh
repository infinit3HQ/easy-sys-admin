#!/usr/bin/env bash
set -euo pipefail

tag=${RELEASE_VERSION:?Set RELEASE_VERSION}
repository=${GITHUB_REPOSITORY:?Set GITHUB_REPOSITORY}
if [[ ! "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || (( ${#tag} > 128 )); then
  echo 'Use a stable version tag in vMAJOR.MINOR.PATCH format.' >&2
  exit 1
fi
if ! sha=$(git rev-parse --verify "refs/tags/$tag^{commit}") || ! git merge-base --is-ancestor "$sha" HEAD; then
  echo 'The release tag must point to a commit in main.' >&2
  exit 1
fi
if gh release view "$tag" --repo "$repository" >/dev/null 2>&1; then
  echo "Release $tag already exists."
  exit 0
fi

registry="ghcr.io/$(printf '%s' "$repository" | tr '[:upper:]' '[:lower:]')"
images=("$registry-control-plane" "$registry-web")
digests=()
needs_tag=()
inspect_digest() {
  docker buildx imagetools inspect "$1" --format '{{.Manifest.Digest}}'
}

# Check both images and existing version tags before writing anything.
for i in "${!images[@]}"; do
  image=${images[$i]}
  if ! digest=$(inspect_digest "$image:sha-$sha") || [[ ! "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo "No verified main build for $image at $sha. Wait for main CI to publish both images." >&2
    exit 1
  fi
  digests[$i]=$digest
  needs_tag[$i]=false
  if existing=$(inspect_digest "$image:$tag" 2>&1); then
    if [[ "$existing" != "$digest" ]]; then
      echo "$image:$tag already points to a different digest." >&2
      exit 1
    fi
  elif [[ "$existing" == *": not found" ]]; then
    needs_tag[$i]=true
  else
    echo "$existing" >&2
    exit 1
  fi
done

for i in "${!images[@]}"; do
  if [[ "${needs_tag[$i]}" == true ]]; then
    docker buildx imagetools create --tag "${images[$i]}:$tag" "${images[$i]}@${digests[$i]}"
  fi
done

notes=$(mktemp)
trap 'rm -f "$notes"' EXIT
{
  printf 'Container images for Linux AMD64 and ARM64. Set both image references in the [self-hosting guide](https://easysysadmin.app/docs/self-hosting).\n\n'
  printf '| Version tag | Digest reference |\n| --- | --- |\n'
  for i in "${!images[@]}"; do
    image=${images[$i]}
    if [[ $(inspect_digest "$image:$tag") != "${digests[$i]}" ]]; then
      echo "$image:$tag does not match the verified build." >&2
      exit 1
    fi
    printf '| `%s:%s` | `%s@%s` |\n' "$image" "$tag" "$image" "${digests[$i]}"
  done
} > "$notes"
gh release create "$tag" --repo "$repository" --verify-tag --title "$tag" --generate-notes --notes-file "$notes"
