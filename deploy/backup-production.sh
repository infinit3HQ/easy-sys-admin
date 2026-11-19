#!/usr/bin/env bash
# Stop deployment on a failed database backup. Keep dumps on the deploy host;
# never upload database contents as GitHub Actions artifacts.
set -euo pipefail
[ "${DEPLOY_ENVIRONMENT:-}" = production ] || exit 1
root=$(cd "$(dirname "$0")/.." && pwd)
compose="$root/deploy/run.sh"
containers=$("$compose" ps --all -q postgres)
if [ -z "$containers" ]; then
  volumes=$(docker volume ls -q --filter label=com.docker.compose.project=esa --filter label=com.docker.compose.volume=postgres)
  [ -z "$volumes" ] || { echo 'Existing database storage has no container; deployment stopped.' >&2; exit 1; }
  exit 0
fi
backup_dir=${ESA_DEPLOY_BACKUP_DIR:-$HOME/.local/state/easy-sys-admin/production/backups}
keep=${ESA_BACKUP_KEEP:-14}
recipient=${ESA_BACKUP_AGE_RECIPIENT:-}
case "$keep" in ''|*[!0-9]*|0) echo 'ESA_BACKUP_KEEP must be a whole number of at least 1.' >&2; exit 1;; esac
if [ -n "$recipient" ]; then
  command -v age >/dev/null 2>&1 || { echo 'ESA_BACKUP_AGE_RECIPIENT is set but age is not installed. Install age or unset it; nothing was written.' >&2; exit 1; }
fi
umask 077
mkdir -p "$backup_dir"
suffix=dump
[ -z "$recipient" ] || suffix=dump.age
backup="$backup_dir/$(date -u +%Y%m%dT%H%M%SZ)-$GITHUB_RUN_ID.$suffix"
trap 'rm -f "$backup.tmp"' EXIT
if [ -n "$recipient" ]; then
  # Encrypted to a public key. Keep the private key off this host.
  "$compose" exec -T postgres pg_dump -U esa -d esa --format=custom | age -r "$recipient" > "$backup.tmp"
else
  "$compose" exec -T postgres pg_dump -U esa -d esa --format=custom > "$backup.tmp"
fi
[ -s "$backup.tmp" ] || { echo 'Database backup was empty; deployment stopped.' >&2; exit 1; }
mv "$backup.tmp" "$backup"
printf 'Pre-deployment database backup saved to %s on Server One.\n' "$backup"
# Keep the newest ESA_BACKUP_KEEP dumps. Names start with a UTC timestamp, so
# name order is age order. Only files this script names are considered.
old=$(ls -1 "$backup_dir" | grep -E '^[0-9]{8}T[0-9]{6}Z-[0-9]+\.dump(\.age)?$' | sort -r | tail -n "+$((keep + 1))" || true)
for name in $old; do
  rm -f -- "$backup_dir/$name"
done
