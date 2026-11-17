#!/usr/bin/env bash
# Claim the first account before publishing the new web service. Keep the
# initial password on the deploy host, never in Actions output or artifacts.
set -euo pipefail
[ "${DEPLOY_ENVIRONMENT:-}" = nightly ] || exit 1
root=$(cd "$(dirname "$0")/.." && pwd)
compose="$root/deploy/run.sh"
count=$("$compose" exec -T postgres psql -U esa -d esa -Atc 'SELECT count(*) FROM users')
if [ "$count" != 0 ]; then exit 0; fi
email=${ESA_ALLOWED_EMAILS:?set ESA_ALLOWED_EMAILS}
case "$email" in *','*|*' '*|*'\n'*) echo 'Initial nightly bootstrap needs exactly one allowed email.' >&2; exit 1;; esac
state="$HOME/.local/state/easy-sys-admin/nightly"
umask 077
mkdir -p "$state"
password_file="$state/bootstrap-password"
if [ ! -f "$password_file" ]; then openssl rand -hex 24 > "$password_file"; fi
# Pass the password on stdin, not as a process argument or a log message.
{
  printf '%s\n' "$email"
  cat "$password_file"
} | "$compose" run --rm --no-deps -T --label traefik.enable=false --entrypoint node web -e '
const fs = require("node:fs");
const [email, password] = fs.readFileSync(0,"utf8").trim().split("\n");
(async () => {
  const result = await fetch("http://esa:8080/auth/password/signup", {
    method:"POST", redirect:"manual",
    headers:{"content-type":"application/x-www-form-urlencoded"},
    body:new URLSearchParams({name:"Nightly admin",email,password}),
  });
  const location=result.headers.get("location") || "";
  if(result.status!==303 || location.includes("error=")) {
    console.error("Nightly bootstrap failed. No credentials were printed.");
    process.exit(1);
  }
})();'
printf 'Initial nightly credentials are stored in %s on the deploy host.\n' "$password_file"
