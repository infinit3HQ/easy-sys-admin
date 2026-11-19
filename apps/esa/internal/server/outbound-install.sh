#!/bin/sh
# Explicit root bootstrap for a server that will connect out to esa.
# Review this file before running it. It installs/replaces the helper and agent.
set -eu
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
[ "$(id -u)" = 0 ] || { echo 'Run this installer as root.' >&2; exit 1; }
origin=${1:?Pass the HTTPS origin of your esa installation.}
case "$origin" in https://*) ;; *) echo 'An HTTPS origin is required.' >&2; exit 1;; esac
# No path, query, credentials, whitespace, or shell syntax in the origin.
host=${origin#https://}
case "$host" in ''|*[!a-zA-Z0-9.:-]*|*:*:*) echo 'Use an HTTPS DNS name with an optional port.' >&2; exit 1;; esac
command -v curl >/dev/null
command -v sudo >/dev/null
command -v systemctl >/dev/null
command -v sha256sum >/dev/null
case "$(uname -m)" in x86_64) arch=amd64;; aarch64|arm64) arch=arm64;; *) echo 'Supported architectures: amd64 and arm64.' >&2; exit 1;; esac
umask 077
tmp=$(mktemp -d)
trap 'stty echo </dev/tty 2>/dev/null || true; rm -rf "$tmp"' EXIT HUP INT TERM
curl --proto '=https' --tlsv1.2 -fsS "$origin/enroll/outbound/binaries/helper/$arch" -o "$tmp/helper"
curl --proto '=https' --tlsv1.2 -fsS "$origin/enroll/outbound/binaries/agent/$arch" -o "$tmp/agent"
# The server that sends these files cannot vouch for them. To pin them, set
# ESA_HELPER_SHA256 and ESA_AGENT_SHA256 to the values for your architecture
# in the release notes. Without them the hashes are printed so you can compare.
helper_sum=$(sha256sum "$tmp/helper" | cut -d' ' -f1)
agent_sum=$(sha256sum "$tmp/agent" | cut -d' ' -f1)
printf 'helper sha256 %s\nagent  sha256 %s\n' "$helper_sum" "$agent_sum"
if [ -n "${ESA_HELPER_SHA256:-}" ] && [ "$ESA_HELPER_SHA256" != "$helper_sum" ]; then echo 'The helper does not match ESA_HELPER_SHA256. Nothing was installed.' >&2; exit 1; fi
if [ -n "${ESA_AGENT_SHA256:-}" ] && [ "$ESA_AGENT_SHA256" != "$agent_sum" ]; then echo 'The agent does not match ESA_AGENT_SHA256. Nothing was installed.' >&2; exit 1; fi
if id esa >/dev/null 2>&1; then
 [ "$(getent passwd esa | cut -d: -f5,6)" = 'easy-sys-admin control plane:/var/lib/esa-home' ] || { echo 'A user named esa exists and was not created by easy-sys-admin. Refusing to reuse it.' >&2; exit 1; }
else
 useradd --system --no-create-home --home-dir /var/lib/esa-home --shell /usr/sbin/nologin --comment 'easy-sys-admin control plane' esa
fi
[ "$(id -u esa)" != 0 ] || { echo 'The esa user must not be root.' >&2; exit 1; }
for path in /var/lib/esa /var/lib/esa-home /usr/local/lib/esa /etc/esa /etc/esa/agent.token /etc/esa/agent.env /etc/systemd/system/esa-agent.service /etc/sudoers.d/esa; do
 [ ! -L "$path" ] || { echo 'Refusing a symlink at an installation path.' >&2; exit 1; }
done
install -d -o root -g root -m 0755 /usr/local/lib/esa /var/lib/esa-home
install -d -o root -g root -m 0750 /var/lib/esa
install -d -o root -g esa -m 0750 /etc/esa
printf 'Paste the one-time agent credential: ' >/dev/tty
stty -echo </dev/tty
IFS= read -r token </dev/tty
stty echo </dev/tty
printf '\n' >/dev/tty
case "$token" in esa_agent_*) ;; *) echo 'Invalid agent credential.' >&2; exit 1;; esac
case "$token" in *[!a-zA-Z0-9_-]* ) echo 'Invalid agent credential.' >&2; exit 1;; esac
[ "${#token}" -le 200 ] || exit 1
printf 'Paste the signing pin shown with it: ' >/dev/tty
IFS= read -r pin </dev/tty
case "$pin" in esa_pin_[0-9]*_*) ;; *) echo 'Invalid signing pin.' >&2; exit 1;; esac
case "$pin" in *[!a-zA-Z0-9_-]* ) echo 'Invalid signing pin.' >&2; exit 1;; esac
[ "${#pin}" -le 200 ] || exit 1
printf '%s\n' "$token" > "$tmp/token"
unset token
install -o root -g esa -m 0640 "$tmp/token" /etc/esa/agent.token
printf 'ESA_AGENT_URL=%s\nESA_AGENT_TOKEN_FILE=/etc/esa/agent.token\nESA_AGENT_PIN=%s\n' "$origin" "$pin" > "$tmp/env"
install -o root -g root -m 0600 "$tmp/env" /etc/esa/agent.env
printf '%s\n' 'Defaults:esa !requiretty' 'Defaults:esa env_reset' 'Defaults:esa secure_path="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"' 'esa ALL=(root) NOPASSWD: /usr/local/lib/esa/esa-helper *' > "$tmp/sudoers"
visudo -cf "$tmp/sudoers"
install -o root -g root -m 0440 "$tmp/sudoers" /etc/sudoers.d/esa
install -o root -g root -m 0755 "$tmp/helper" /usr/local/lib/esa/esa-helper
install -o root -g root -m 0755 "$tmp/agent" /usr/local/lib/esa/esa-agent
cat > "$tmp/unit" <<'UNIT'
[Unit]
Description=esa outbound agent
After=network-online.target
Wants=network-online.target
[Service]
User=esa
Group=esa
EnvironmentFile=/etc/esa/agent.env
ExecStart=/usr/local/lib/esa/esa-agent
Restart=on-failure
RestartSec=10
UMask=0077
[Install]
WantedBy=multi-user.target
UNIT
install -o root -g root -m 0644 "$tmp/unit" /etc/systemd/system/esa-agent.service
systemctl daemon-reload
systemctl enable esa-agent.service
systemctl restart esa-agent.service
echo 'Outbound agent installed. It needs outgoing HTTPS only. Existing SSH settings were kept.'
