package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// Every command Pool sends must pass the esa key's forced command unchanged.
func TestHelperCommandPassesDispatch(t *testing.T) {
	args := []string{"--unit", "nginx.service", "--grep", "it's a $(test)", "", "--lines", "200", "a\nb"}
	got, err := hostops.DispatchArgs(helperCommand("logs", args...))
	if err != nil {
		t.Fatal(err)
	}
	want := append([]string{"-n", proto.HelperPath, "logs"}, args...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, s := range []string{"status", "a b", "it's", "''", `\'`, "'''", "$(reboot)", "x;rm -rf /", "--journal-size", "é"} {
		words, err := hostops.SplitShellWords(ShellQuote(s))
		if err != nil || len(words) != 1 || words[0] != s {
			t.Errorf("round trip of %q: %q, %v", s, words, err)
		}
	}
}

func TestInstallScriptLocksDownHome(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	script, err := InstallScript(MarshalKey(signer.PublicKey()))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`restrict,command="` + proto.HelperPath + ` ssh-dispatch" ssh-ed25519 `,
		`chown root:root "$home"`,
		`rm -rf "$home/.ssh"`,
		`chmod 0644 "$home/.ssh/authorized_keys.new"`,
		"ESA_INSTALL_ERROR a user named esa already exists",
		"Defaults:esa env_reset",
		`Defaults:esa secure_path="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"`,
		"home=" + proto.UserHome + "\n",
		// The helper is checked on the root-owned copy, not the upload.
		`sha256sum -c -`,
		`if [ -L "$lib" ]`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("install script lacks %q", want)
		}
	}
	for _, bad := range []string{"chown esa", "-o esa", "getent passwd esa | cut -d: -f6)", "--create-home"} {
		if strings.Contains(script, bad) {
			t.Errorf("install script still has %q", bad)
		}
	}
	if strings.Index(script, `sha256sum -c -`) < strings.Index(script, `install -m 0755 -o root -g root "$src" "$lib/esa-helper.new"`) ||
		strings.Index(script, `sha256sum -c -`) > strings.Index(script, `mv -f "$lib/esa-helper.new" "$lib/esa-helper"`) {
		t.Error("the helper checksum must be checked on the root-owned copy, before it replaces the helper")
	}
	// The root-owned home must be in place before anything is written in it.
	if strings.Index(script, `chown root:root "$home"`) > strings.Index(script, `rm -rf "$home/.ssh"`) {
		t.Error("home must be root-owned before .ssh is replaced")
	}
	if hostops.SafePath != "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin" {
		t.Error("sudo secure_path and the helper PATH must match")
	}
}

func TestScrubRemovesPassword(t *testing.T) {
	if got := scrub("sh: 1: hunter2: not found", "hunter2"); strings.Contains(got, "hunter2") {
		t.Fatalf("password left in %q", got)
	}
	if got := scrub("plain", ""); got != "plain" {
		t.Fatalf("empty password changed text: %q", got)
	}
}

// The script file is written by the login user. It must run only if it still
// has the hash that was computed before it was uploaded.
func TestSudoVerifiedScriptRunsOnlyUnchangedScript(t *testing.T) {
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Skip("sha256sum is not installed")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stand-in sudo that drops "-S -p ''" and runs the rest.
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte("#!/bin/sh\nshift 3\nexec \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "script.sh")
	body := []byte("echo ran:$1:$2\n")
	if err := os.WriteFile(script, body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	run := func() (string, error) {
		cmd := exec.Command("sh", "-c", sudoVerifiedScriptCommand(script, "/tmp/upload", hex.EncodeToString(sum[:]), "HELPERSUM"))
		cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := run()
	if err != nil || strings.TrimSpace(out) != "ran:/tmp/upload:HELPERSUM" {
		t.Fatalf("unchanged script: %q %v", out, err)
	}
	if err := os.WriteFile(script, append(body, []byte("echo pwned\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = run()
	if err == nil || strings.Contains(out, "pwned") || strings.Contains(out, "ran:") || !strings.Contains(out, "ESA_INSTALL_ERROR") {
		t.Fatalf("a changed script ran or failed without saying why: %q %v", out, err)
	}
}
