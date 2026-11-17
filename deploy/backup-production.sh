#!/usr/bin/env bash
# Stop deployment on a failed database backup. Keep dumps on the deploy host;
# never upload database contents as GitHub Actions artifacts.
set -euo pipefail
[ "${DEPLOY_ENVIRONMENT:-}" = production ] || exit 1
root=$(cd "$(dirname "$0")/.." && pwd)
compose="$root/deploy/run.sh"
if [ -z "$("$compose" ps -q postgres)" ]; then exit 0; fi
backup_dir=${ESA_DEPLOY_BACKUP_DIR:-$HOME/.local/state/easy-sys-admin/production/backups}
umask 077
mkdir -p "$backup_dir"
backup="$backup_dir/$(date -u +%Y%m%dT%H%M%SZ)-$GITHUB_RUN_ID.dump"
trap 'rm -f "$backup.tmp"' EXIT
"$compose" exec -T postgres pg_dump -U esa -d esa --format=custom > "$backup.tmp"
[ -s "$backup.tmp" ] || { echo 'Database backup was empty; deployment stopped.' >&2; exit 1; }
mv "$backup.tmp" "$backup"
printf 'Pre-deployment database backup saved to %s on Server One.\n' "$backup"
