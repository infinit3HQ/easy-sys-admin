package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// testServer is an in-process SSH server that counts authentication attempts.
type testServer struct {
	addr     string
	hostKey  ssh.PublicKey
	attempts atomic.Int32
}

func startServer(t *testing.T) *testServer {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	ts := &testServer{hostKey: signer.PublicKey()}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
			ts.attempts.Add(1)
			return nil, errors.New("no")
		},
		PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) {
			ts.attempts.Add(1)
			return nil, errors.New("no")
		},
	}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	ts.addr = ln.Addr().String()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _, _, _ = ssh.NewServerConn(c, cfg)
				c.Close()
			}()
		}
	}()
	return ts
}

func (ts *testServer) hostPort(t *testing.T) (string, int) {
	h, p, _ := net.SplitHostPort(ts.addr)
	port, _ := strconv.Atoi(p)
	return h, port
}

// A changed host key must abort before any password or key is offered.
func TestDialRefusesChangedHostKeyBeforeAuth(t *testing.T) {
	ts := startServer(t)
	host, port := ts.hostPort(t)
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	wrong, _ := ssh.NewPublicKey(other)

	_, err := Dial(context.Background(), Target{
		Host: host, Port: port, User: "root", HostKey: wrong,
		Auth: []ssh.AuthMethod{ssh.Password("secret")},
	})
	var mismatch *HostKeyMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("want HostKeyMismatchError, got %v", err)
	}
	if ssh.FingerprintSHA256(mismatch.Presented) != ssh.FingerprintSHA256(ts.hostKey) {
		t.Fatal("mismatch error must carry the presented key")
	}
	if n := ts.attempts.Load(); n != 0 {
		t.Fatalf("credentials were offered %d times to a host with a changed key", n)
	}
}

func TestDialRequiresPinnedKey(t *testing.T) {
	if _, err := Dial(context.Background(), Target{Host: "127.0.0.1", Port: 22}); err == nil {
		t.Fatal("dial without a host key must fail")
	}
}

func TestProbeSendsNoCredentials(t *testing.T) {
	ts := startServer(t)
	host, port := ts.hostPort(t)
	k, err := ProbeHostKey(context.Background(), host, port)
	if err != nil {
		t.Fatal(err)
	}
	if ssh.FingerprintSHA256(k) != ssh.FingerprintSHA256(ts.hostKey) {
		t.Fatal("probe returned the wrong key")
	}
	if n := ts.attempts.Load(); n != 0 {
		t.Fatalf("probe made %d authentication attempts", n)
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"status":         "status",
		"nginx.service":  "nginx.service",
		"a b":            "'a b'",
		"it's":           `'it'\''s'`,
		"$(reboot)":      "'$(reboot)'",
		"x;rm -rf /":     "'x;rm -rf /'",
		"":               "''",
		"--journal-size": "--journal-size",
	}
	for in, want := range cases {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInstallScriptRejectsBadKey(t *testing.T) {
	if _, err := InstallScript("ssh-ed25519 AAAA' ; reboot ; '"); err == nil {
		t.Fatal("a malformed account key must not reach the script")
	}
}

func TestConnectTimeoutFromContext(t *testing.T) {
	ctx := context.Background()
	if got := connectTimeout(ctx); got != DefaultConnectTimeout {
		t.Errorf("default: %s", got)
	}
	if got := connectTimeout(WithConnectTimeout(ctx, 2*time.Second)); got != 2*time.Second {
		t.Errorf("set: %s", got)
	}
	if got := connectTimeout(WithConnectTimeout(ctx, 0)); got != DefaultConnectTimeout {
		t.Errorf("zero keeps the default: %s", got)
	}
}

// A server that accepts TCP but never speaks SSH is cut off by the
// caller's deadline, not only by the handshake timeout.
func TestDialHonoursContextDuringHandshake(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { c.Close() })
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(p)
	k, _, _ := ed25519.GenerateKey(rand.Reader)
	hk, _ := ssh.NewPublicKey(k)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := Dial(ctx, Target{Host: h, Port: port, User: "esa", HostKey: hk}); err == nil {
		t.Fatal("dial to a silent server succeeded")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("dial took %s; the context deadline should end it", d)
	}
}
