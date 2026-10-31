package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// InstallScript is the shell script that sets a server up for esa. It is
// shared by the enroll command (path A) and the control plane's own install
// over a sudo login (path B). It expects the new helper binary at "$1".
//
// It creates a system user "esa" with no usable password, allows the
// account's key with the "restrict" option (no pty, no forwarding) and a
// forced command that only passes helper calls on to sudo, installs the
// helper, and allows exactly one sudo command: the helper.
//
// The esa home and its .ssh are owned by root, so the esa user cannot
// write there; nothing esa runs needs to. An existing "esa" user is only
// reused when it carries our comment and home.
func InstallScript(accountPublicKey string) (string, error) {
	k, err := ParseKey(accountPublicKey)
	if err != nil {
		return "", fmt.Errorf("account key: %w", err)
	}
	keyLine := `restrict,command="` + proto.HelperPath + ` ssh-dispatch" ` + MarshalKey(k) + " esa-control-plane"
	return `set -eu
umask 022
src="$1"
lib=/usr/local/lib/esa
home=` + proto.UserHome + `
comment="` + proto.UserComment + `"
if [ "$(id -u)" != 0 ]; then echo "ESA_INSTALL_ERROR must run as root" >&2; exit 1; fi
if ! command -v sudo >/dev/null 2>&1; then echo "ESA_INSTALL_ERROR sudo is not installed" >&2; exit 1; fi
if id esa >/dev/null 2>&1; then
  if [ "$(getent passwd esa | cut -d: -f5,6)" != "$comment:$home" ]; then
    echo "ESA_INSTALL_ERROR a user named esa already exists and was not created by easy-sys-admin; remove or rename it, then try again" >&2; exit 1
  fi
else
  useradd --system --no-create-home --home-dir "$home" --shell /bin/sh --comment "$comment" esa
fi
# "*" is no valid password but, unlike "!", does not lock key logins.
usermod -p '*' esa
if [ -L "$home" ] || { [ -e "$home" ] && [ ! -d "$home" ]; }; then
  echo "ESA_INSTALL_ERROR $home is not a directory" >&2; exit 1
fi
# Root owns the home first, so esa cannot plant links in it while we work.
mkdir -p "$home"
chown root:root "$home"
chmod 0755 "$home"
install -d -m 0755 -o root -g root "$lib"
install -m 0755 -o root -g root "$src" "$lib/esa-helper.new"
mv -f "$lib/esa-helper.new" "$lib/esa-helper"
rm -f "$src"
rm -rf "$home/.ssh"
mkdir -m 0755 "$home/.ssh"
chown root:root "$home/.ssh"
printf '%s\n' '` + keyLine + `' > "$home/.ssh/authorized_keys.new"
chmod 0644 "$home/.ssh/authorized_keys.new"
mv -f "$home/.ssh/authorized_keys.new" "$home/.ssh/authorized_keys"
# sudo ignores files in sudoers.d whose name contains a dot, so the .new file is inert until renamed.
printf '%s\n' 'Defaults:esa !requiretty' 'Defaults:esa env_reset' 'Defaults:esa secure_path="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"' 'esa ALL=(root) NOPASSWD: ` + proto.HelperPath + ` *' > /etc/sudoers.d/esa.new
chmod 0440 /etc/sudoers.d/esa.new
visudo -cf /etc/sudoers.d/esa.new >/dev/null
mv -f /etc/sudoers.d/esa.new /etc/sudoers.d/esa
install -d -m 0750 -o root -g root ` + proto.StateDir + `
echo ESA_INSTALL_OK
`, nil
}

// LoginAuth is a one-time sudo-capable login used to install the helper.
type LoginAuth struct {
	User       string
	Password   string // password login, also used for sudo when not root
	PrivateKey []byte // PEM private key login
	Passphrase string // for an encrypted private key
}

// AuthMethods turns the login into SSH auth methods.
func (l LoginAuth) AuthMethods() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if len(l.PrivateKey) > 0 {
		var signer ssh.Signer
		var err error
		if l.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(l.PrivateKey, []byte(l.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(l.PrivateKey)
		}
		if err != nil {
			var missing *ssh.PassphraseMissingError
			if errors.As(err, &missing) {
				return nil, errors.New("the private key is encrypted; enter its passphrase")
			}
			return nil, fmt.Errorf("private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if l.Password != "" {
		pw := l.Password
		methods = append(methods, ssh.Password(pw), ssh.KeyboardInteractive(
			func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = pw
				}
				return answers, nil
			}))
	}
	if len(methods) == 0 {
		return nil, errors.New("give a password or a private key")
	}
	return methods, nil
}

// InstallRequest is everything Install needs.
type InstallRequest struct {
	Host    string
	Port    int
	HostKey ssh.PublicKey
	Login   LoginAuth
	// AccountPublicKey goes into esa's authorized_keys.
	AccountPublicKey string
	// Helper returns the helper binary for a GOARCH ("amd64", "arm64").
	Helper func(arch string) ([]byte, error)
}

// InstallResult reports what was installed.
type InstallResult struct {
	Arch string
}

// Install logs in with a sudo-capable login and runs InstallScript. The
// login is used only for this; afterwards the control plane uses the esa
// user and the account key.
func Install(ctx context.Context, req InstallRequest) (InstallResult, error) {
	auth, err := req.Login.AuthMethods()
	if err != nil {
		return InstallResult{}, err
	}
	client, err := Dial(ctx, Target{Host: req.Host, Port: req.Port, User: req.Login.User, Auth: auth, HostKey: req.HostKey})
	if err != nil {
		return InstallResult{}, err
	}
	defer client.Close()

	run := func(cmd string, stdin []byte) (string, error) {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		res, err := Exec(cctx, client, cmd, bytes.NewReader(stdin))
		if err != nil {
			return "", scrubErr(err, req.Login.Password)
		}
		if res.Exit != 0 {
			return "", errors.New(scrub(cleanInstallError(res), req.Login.Password))
		}
		return strings.TrimSpace(string(res.Stdout)), nil
	}

	uname, err := run("uname -sm", nil)
	if err != nil {
		return InstallResult{}, err
	}
	arch, err := goArch(uname)
	if err != nil {
		return InstallResult{}, err
	}
	uid, err := run("id -u", nil)
	if err != nil {
		return InstallResult{}, err
	}

	bin, err := req.Helper(arch)
	if err != nil {
		return InstallResult{}, err
	}
	script, err := InstallScript(req.AccountPublicKey)
	if err != nil {
		return InstallResult{}, err
	}

	rnd := make([]byte, 8)
	_, _ = rand.Read(rnd)
	tmp := "/tmp/esa-helper-upload-" + hex.EncodeToString(rnd)
	if _, err := run("umask 077 && cat > "+tmp, bin); err != nil {
		return InstallResult{}, fmt.Errorf("upload helper: %w", err)
	}

	var cmd string
	var stdin []byte
	switch {
	case uid == "0":
		cmd, stdin = "sh -s -- "+tmp, []byte(script)
	case req.Login.Password == "":
		cmd, stdin = "sudo -n sh -s -- "+tmp, []byte(script)
	default:
		if _, err := run("sudo -n true", nil); err == nil {
			cmd, stdin = "sudo -n sh -s -- "+tmp, []byte(script)
			break
		}
		// sudo needs the password. stdin then carries only the password,
		// and sh reads the script from a file with stdin from /dev/null, so
		// the password never reaches a shell even when sudo does not ask.
		scriptTmp := tmp + ".sh"
		if _, err := run("umask 077 && cat > "+scriptTmp, []byte(script)); err != nil {
			_, _ = run("rm -f "+tmp, nil)
			return InstallResult{}, fmt.Errorf("upload install script: %w", err)
		}
		defer func() { _, _ = run("rm -f "+scriptTmp, nil) }()
		cmd = `sudo -S -p '' sh -c 'exec sh "$1" "$2" </dev/null' sh ` + scriptTmp + " " + tmp
		stdin = []byte(req.Login.Password + "\n")
	}
	out, err := run(cmd, stdin)
	if err != nil {
		_, _ = run("rm -f "+tmp, nil)
		return InstallResult{}, fmt.Errorf("install: %w", err)
	}
	if !strings.Contains(out, "ESA_INSTALL_OK") {
		return InstallResult{}, fmt.Errorf("install script did not finish: %s", scrub(out, req.Login.Password))
	}
	return InstallResult{Arch: arch}, nil
}

// scrub removes the login password from text that may end up in errors.
func scrub(s, password string) string {
	if password == "" {
		return s
	}
	return strings.ReplaceAll(s, password, "[password]")
}

func scrubErr(err error, password string) error {
	if password == "" || !strings.Contains(err.Error(), password) {
		return err
	}
	return errors.New(scrub(err.Error(), password))
}

func cleanInstallError(res ExecResult) string {
	msg := strings.TrimSpace(string(res.Stderr))
	if i := strings.Index(msg, "ESA_INSTALL_ERROR "); i >= 0 {
		msg = msg[i+len("ESA_INSTALL_ERROR "):]
	}
	switch {
	case strings.Contains(msg, "a password is required"), strings.Contains(msg, "incorrect password"), strings.Contains(msg, "Sorry, try again"), strings.Contains(msg, "no password was provided"):
		return "sudo needs a password; log in with a password, or use root"
	case strings.Contains(msg, "is not in the sudoers"), strings.Contains(msg, "not allowed to"):
		return "this user cannot use sudo"
	case msg == "":
		return fmt.Sprintf("exit status %d", res.Exit)
	}
	if len(msg) > 400 {
		msg = msg[:400]
	}
	return msg
}

func goArch(uname string) (string, error) {
	f := strings.Fields(uname)
	if len(f) != 2 || f[0] != "Linux" {
		return "", fmt.Errorf("unsupported system %q; esa supports Linux on amd64 and arm64", uname)
	}
	switch f[1] {
	case "x86_64", "amd64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	}
	return "", fmt.Errorf("unsupported CPU %q; esa supports amd64 and arm64", f[1])
}

// Helper updates also go through Install with a sudo login. The helper has
// no self-update action on purpose: with one, the esa key could install any
// binary as root, while today it can only run the helper's fixed actions.
