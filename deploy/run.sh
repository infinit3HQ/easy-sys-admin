#!/usr/bin/env bash
# Use only the image deployment files and the environment selected by CI.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
files=(-f "$root/deploy/compose.yaml" -f "$root/deploy/compose.images.yaml")
case "${DEPLOY_ENVIRONMENT:?set DEPLOY_ENVIRONMENT}" in
  production) ;;
  nightly) files+=(-f "$root/deploy/compose.nightly.yaml");;
  *) echo 'Unsupported deployment environment.' >&2; exit 1;;
esac
exec docker compose "${files[@]}" --env-file "${ENV_FILE:?set ENV_FILE}" "$@"
