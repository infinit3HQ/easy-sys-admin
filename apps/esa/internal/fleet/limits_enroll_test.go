package fleet

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBin writes small stand-ins for uname and curl. curl writes body to
// the -o file, so the "downloaded helper" is whatever the test wants.
func fakeBin(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, script string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("uname", `echo x86_64; echo called > "$ESA_TEST_DIR/uname-called"`+"\n")
	write("curl", `out=""; while [ $# -gt 0 ]; do [ "$1" = -o ] && out="$2"; shift; done
[ -n "$out" ] && printf '%s' '`+body+`' > "$out"; printf 204
`)
	return dir
}

func runScript(t *testing.T, script, bin string) (string, error) {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	f := filepath.Join(t.TempDir(), "enroll.sh")
	if err := os.WriteFile(f, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sh, f)
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "ESA_TEST_DIR="+bin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestEnrollScriptTruncatedRunsNothing(t *testing.T) {
	script := enrollScript("https://esa.example.com/enroll/enr_x", "echo installed\n", map[string]string{"amd64": "00", "arm64": "00"})
	lines := strings.SplitAfter(script, "\n")
	bin := fakeBin(t, "x")
	for i := 1; i < len(lines)-2; i++ { // every prefix without the final call
		_ = os.Remove(filepath.Join(bin, "uname-called"))
		_, _ = runScript(t, strings.Join(lines[:i], ""), bin)
		if _, err := os.Stat(filepath.Join(bin, "uname-called")); err == nil {
			t.Fatalf("a script cut after %d lines ran commands", i)
		}
	}
	_, _ = runScript(t, script, bin)
	if _, err := os.Stat(filepath.Join(bin, "uname-called")); err != nil {
		t.Fatal("the whole script should run (the check above would pass vacuously)")
	}
}

func TestEnrollScriptChecksHelper(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "installed")
	install := "echo ran > '" + marker + "'\n"
	good := sha256.Sum256([]byte("helper"))
	sums := map[string]string{"amd64": hex.EncodeToString(good[:]), "arm64": ""}
	script := enrollScript("https://esa.example.com/enroll/enr_x", install, sums)

	out, err := runScript(t, script, fakeBin(t, "tampered"))
	if err == nil || !strings.Contains(out, "does not match its checksum") {
		t.Fatalf("tampered helper: err=%v out=%s", err, out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("install ran despite a checksum mismatch")
	}

	out, err = runScript(t, script, fakeBin(t, "helper"))
	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("matching helper was not installed: err=%v out=%s", err, out)
	}
}

func TestEnrollURLOK(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://esa.example.com": true, "http://localhost:3000": true, "http://127.0.0.1:8080": true,
		"http://[::1]:3000": true, "http://esa.example.com": false, "http://10.0.0.5:3000": false,
	} {
		u, _ := url.Parse(raw)
		s := &Service{publicURL: u}
		if err := s.enrollURLOK(); (err == nil) != ok {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

func TestBackoffDelay(t *testing.T) {
	want := map[int]string{1: "0s", 2: "0s", 3: "2m0s", 4: "4m0s", 5: "8m0s", 6: "16m0s", 7: "30m0s", 50: "30m0s"}
	for n, d := range want {
		if got := backoffDelay(n).String(); got != d {
			t.Errorf("backoffDelay(%d) = %s, want %s", n, got, d)
		}
	}
}
