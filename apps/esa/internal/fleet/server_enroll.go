package fleet

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const enrollTTL = 15 * time.Minute

// Enrollment is path A: a one-time command the person pastes on the server.
type Enrollment struct {
	Server    ServerView
	Command   string
	ScriptURL string
	ExpiresAt time.Time
}

// EnrollInput is the add-server form for path A.
type EnrollInput struct {
	Name              string
	Host              string
	Port              int
	GroupID           *int64
	HostsControlPlane bool
}

// CreateEnrollment registers a pending server and returns the enroll command.
func (s *Service) CreateEnrollment(ctx context.Context, a Actor, in EnrollInput) (Enrollment, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return Enrollment{}, err
	}
	if err := s.enrollURLOK(); err != nil {
		return Enrollment{}, err
	}
	srv, _, err := s.validateNewServer(ctx, a, in.Name, in.Host, in.Port, in.GroupID, "")
	if err != nil {
		return Enrollment{}, err
	}
	srv.HostsControlPlane = in.HostsControlPlane
	token, hash := randomToken("enr_")
	expires := s.now().Add(enrollTTL)
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		tx := q.Raw()
		if err := tx.Omit(clause.Associations).Create(&srv).Error; err != nil {
			return mapStore(err)
		}
		if err := tx.Create(&store.ServerState{ServerID: srv.ID, Status: string(StatusPending), UpdatedAt: s.now()}).Error; err != nil {
			return err
		}
		tok := store.EnrollToken{TokenHash: hash, AccountID: a.AccountID, ServerID: srv.ID, CreatedBy: a.UserID, ExpiresAt: expires, CreatedAt: s.now()}
		if err := tx.Omit(clause.Associations).Create(&tok).Error; err != nil {
			return err
		}
		s.audit(ctx, q, a, "server.enroll_start", srv.Name, map[string]any{"host": srv.Host, "port": srv.Port})
		return nil
	})
	if err != nil {
		return Enrollment{}, err
	}
	view, err := s.GetServer(ctx, a, srv.ID)
	if err != nil {
		return Enrollment{}, err
	}
	scriptURL := s.publicURL.JoinPath("/enroll", token).String()
	return Enrollment{
		Server: view, ScriptURL: scriptURL, ExpiresAt: expires,
		Command: "curl -fsSL " + scriptURL + " | sudo sh",
	}, nil
}

// NewEnrollToken issues a fresh enroll command for a pending server.
func (s *Service) NewEnrollToken(ctx context.Context, a Actor, serverID int64) (Enrollment, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return Enrollment{}, err
	}
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return Enrollment{}, err
	}
	if srv.Enrollment == "ready" {
		return Enrollment{}, &ConflictError{Message: srv.Name + " is already enrolled"}
	}
	if err := s.enrollURLOK(); err != nil {
		return Enrollment{}, err
	}
	token, hash := randomToken("enr_")
	now := s.now()
	expires := now.Add(enrollTTL)
	tok := store.EnrollToken{TokenHash: hash, AccountID: a.AccountID, ServerID: srv.ID, CreatedBy: a.UserID, ExpiresAt: expires, CreatedAt: now}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		tx := q.Raw()
		// Only the newest command works; older ones stop working.
		if err := tx.Model(&store.EnrollToken{}).Where("server_id = ? AND used_at IS NULL", srv.ID).Update("used_at", now).Error; err != nil {
			return err
		}
		if err := tx.Omit(clause.Associations).Create(&tok).Error; err != nil {
			return err
		}
		s.audit(ctx, q, a, "server.enroll_token", srv.Name, nil)
		return nil
	})
	if err != nil {
		return Enrollment{}, err
	}
	view, err := s.GetServer(ctx, a, srv.ID)
	if err != nil {
		return Enrollment{}, err
	}
	scriptURL := s.publicURL.JoinPath("/enroll", token).String()
	return Enrollment{Server: view, ScriptURL: scriptURL, ExpiresAt: expires, Command: "curl -fsSL " + scriptURL + " | sudo sh"}, nil
}

// enrollURLOK refuses to hand out enroll commands over plain http, except
// for a loopback address used during development. The command pipes the
// script into a root shell, so it must not travel unencrypted.
func (s *Service) enrollURLOK() error {
	if s.publicURL.Scheme == "https" {
		return nil
	}
	h := s.publicURL.Hostname()
	if ip := net.ParseIP(h); h == "localhost" || (ip != nil && ip.IsLoopback()) {
		return nil
	}
	return &ConflictError{Message: "enrolling needs ESA_PUBLIC_URL to be https"}
}

// ErrEnrollTokenInvalid means the token is unknown, used, or expired.
var ErrEnrollTokenInvalid = errors.New("this enroll command has expired or was already used; create a new one in esa")

func (s *Service) enrollToken(ctx context.Context, token string) (store.EnrollToken, store.Server, error) {
	if !strings.HasPrefix(token, "enr_") || len(token) > 100 {
		return store.EnrollToken{}, store.Server{}, ErrEnrollTokenInvalid
	}
	var tok store.EnrollToken
	if err := s.db.WithContext(ctx).Where("token_hash = ?", hashToken(token)).Take(&tok).Error; err != nil {
		return store.EnrollToken{}, store.Server{}, ErrEnrollTokenInvalid
	}
	if tok.UsedAt != nil || !s.now().Before(tok.ExpiresAt) {
		return store.EnrollToken{}, store.Server{}, ErrEnrollTokenInvalid
	}
	var srv store.Server
	if err := s.db.WithContext(ctx).Where("id = ? AND account_id = ?", tok.ServerID, tok.AccountID).Take(&srv).Error; err != nil {
		return store.EnrollToken{}, store.Server{}, ErrEnrollTokenInvalid
	}
	return tok, srv, nil
}

// EnrollScript returns the script served at /enroll/{token}. The script
// downloads the helper with the same token and reports the host key back.
func (s *Service) EnrollScript(ctx context.Context, token string) (string, error) {
	tok, _, err := s.enrollToken(ctx, token)
	if err != nil {
		return "", err
	}
	k, err := s.accountKey(ctx, tok.AccountID)
	if err != nil {
		return "", err
	}
	install, err := remote.InstallScript(k.publicKey)
	if err != nil {
		return "", err
	}
	sums := map[string]string{}
	for _, arch := range []string{"amd64", "arm64"} {
		if bin, err := s.helpers(arch); err == nil {
			sum := sha256.Sum256(bin)
			sums[arch] = hex.EncodeToString(sum[:])
		}
	}
	return enrollScript(s.publicURL.JoinPath("/enroll", token).String(), install, sums), nil
}

// enrollScript renders the enroll script. The body is one function called
// on the last line, so a download cut short runs nothing. sums maps GOARCH
// to the helper's SHA-256; the helper is checked before it is installed.
func enrollScript(base, install string, sums map[string]string) string {
	return `#!/bin/sh
# easy-sys-admin enroll script. It installs esa-helper and lets the esa
# control plane log in as the "esa" user, which may only run esa-helper.
set -eu
esa_enroll() {
base='` + base + `'
case "$(uname -m)" in
  x86_64|amd64) arch=amd64; want='` + sums["amd64"] + `' ;;
  aarch64|arm64) arch=arm64; want='` + sums["arm64"] + `' ;;
  *) echo "esa: unsupported CPU $(uname -m)" >&2; exit 1 ;;
esac
[ -n "$want" ] || { echo "esa: the control plane has no helper for $arch" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "esa: curl is required" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then sha() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then sha() { shasum -a 256 "$1" | cut -d' ' -f1; }
else echo "esa: sha256sum or shasum is required" >&2; exit 1; fi
# A private directory (mode 700), so no other user can swap the files we run as root.
dir=$(mktemp -d /tmp/esa-enroll.XXXXXX)
trap 'rm -rf "$dir"' EXIT
curl -fsSL "$base/helper?arch=$arch" -o "$dir/esa-helper"
got=$(sha "$dir/esa-helper")
[ "$got" = "$want" ] || { echo "esa: the downloaded helper does not match its checksum; nothing was installed" >&2; exit 1; }
cat > "$dir/install.sh" <<'ESA_INSTALL'
` + install + `ESA_INSTALL
sh "$dir/install.sh" "$dir/esa-helper"
hostkey=$(cat /etc/ssh/ssh_host_ed25519_key.pub 2>/dev/null || cat /etc/ssh/ssh_host_ecdsa_key.pub)
code=$(curl -sS -o "$dir/complete" -w '%{http_code}' -X POST -H 'Content-Type: text/plain' --data-binary "$hostkey" "$base/complete") || code=000
if [ "$code" != 204 ]; then
  msg=$(sed -n "s/^echo '\\(.*\\)' >&2; exit 1$/\\1/p" "$dir/complete" 2>/dev/null || true)
  echo "${msg:-esa: could not report this server to esa (HTTP $code)}" >&2
  exit 1
fi
echo "esa: done. This server will appear as ready in esa within a minute."
}
esa_enroll
`
}

// EnrollHelper returns the helper binary for an enroll token.
func (s *Service) EnrollHelper(ctx context.Context, token, arch string) ([]byte, error) {
	if _, _, err := s.enrollToken(ctx, token); err != nil {
		return nil, err
	}
	if arch != "amd64" && arch != "arm64" {
		return nil, invalid("arch", "must be amd64 or arm64")
	}
	return s.helpers(arch)
}

// CompleteEnrollment records the host key the enroll script reported. The
// key arrives over HTTPS together with the one-time token, which is why it
// is trusted without a separate prompt; it is still shown on the server page.
func (s *Service) CompleteEnrollment(ctx context.Context, token, hostKey string) error {
	tok, srv, err := s.enrollToken(ctx, token)
	if err != nil {
		return err
	}
	k, err := remote.ParseKey(strings.TrimSpace(hostKey))
	if err != nil {
		return invalid("hostKey", "not a valid SSH public key")
	}
	if srv.Enrollment == "ready" {
		return ErrEnrollTokenInvalid
	}
	if err := s.confirmHostKey(ctx, srv, k); err != nil {
		return err
	}
	now := s.now()
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		tx := q.Raw()
		res := tx.Model(&store.EnrollToken{}).Where("token_hash = ? AND used_at IS NULL", tok.TokenHash).Update("used_at", now)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrEnrollTokenInvalid
		}
		// Any other command issued for this server stops working too.
		if err := tx.Model(&store.EnrollToken{}).Where("server_id = ? AND used_at IS NULL", srv.ID).Update("used_at", now).Error; err != nil {
			return err
		}
		res = tx.Model(&store.Server{}).Where("id = ? AND enrollment <> 'ready'", srv.ID).Updates(map[string]any{
			"host_key": remote.MarshalKey(k), "host_key_fingerprint": remote.Fingerprint(k),
			"host_key_accepted_by": tok.CreatedBy, "host_key_accepted_at": now,
			"enrollment": "ready", "updated_at": now,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return ErrEnrollTokenInvalid
		}
		s.audit(ctx, q, Actor{AccountID: tok.AccountID, UserID: tok.CreatedBy, Source: SourceSystem},
			"server.enroll_complete", srv.Name, map[string]any{"host_key": remote.Fingerprint(k)})
		return nil
	})
	if err != nil {
		return err
	}
	s.afterEnroll(srv.ID)
	return nil
}

// confirmHostKey checks that the server esa will connect to presents the
// key the enroll script reported. Without this, anyone holding the token
// could report a key for a host they do not control.
func (s *Service) confirmHostKey(ctx context.Context, srv store.Server, reported ssh.PublicKey) error {
	pctx, cancel := context.WithTimeout(s.netCtx(ctx), 20*time.Second)
	defer cancel()
	presented, err := remote.ProbeHostKeyLike(pctx, srv.Host, srv.Port, reported)
	addr := net.JoinHostPort(srv.Host, strconv.Itoa(srv.Port))
	if err != nil {
		s.log.Warn("enroll: probe host key", "server_id", srv.ID, "addr", addr, "err", err)
		return invalid("hostKey", "esa could not reach %s over SSH to confirm this server's host key (%s); check the address and port in esa and that esa can reach it", addr, netErrorText(err))
	}
	if !bytes.Equal(presented.Marshal(), reported.Marshal()) {
		return invalid("hostKey", "the host key at %s (%s) is not this server's key (%s); check the address in esa points to this server", addr, remote.Fingerprint(presented), remote.Fingerprint(reported))
	}
	return nil
}
