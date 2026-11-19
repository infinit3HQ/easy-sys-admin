package fleet

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/netguard"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestLimitRate(t *testing.T) {
	s := &Service{}
	a := Actor{AccountID: 1, Source: SourceWeb}
	for i := range rateLimit {
		if err := s.limitRate(a, "server.probe"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	var conflict *ConflictError
	if err := s.limitRate(a, "server.probe"); !errors.As(err, &conflict) {
		t.Fatalf("call over the limit: %v", err)
	}
	if err := s.limitRate(Actor{AccountID: 2, Source: SourceWeb}, "server.probe"); err != nil {
		t.Fatalf("another account has its own limit: %v", err)
	}
	if err := s.limitRate(a, "cert.add"); err != nil {
		t.Fatalf("another action has its own limit: %v", err)
	}
}

func TestLimitRateIsSharedByTwoProcesses(t *testing.T) {
	s := newTestService(t)
	other := &Service{db: s.db, log: s.log}
	a := Actor{AccountID: 1, Source: SourceWeb}
	for i := range rateLimit {
		caller := s
		if i%2 == 1 {
			caller = other
		}
		if err := caller.limitRate(a, "server.add"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	var conflict *ConflictError
	if err := other.limitRate(a, "server.add"); !errors.As(err, &conflict) {
		t.Fatalf("the second process was given its own quota: %v", err)
	}
}

func TestNetErrorText(t *testing.T) {
	blocked := netguard.Check(netip.MustParseAddr("169.254.169.254"), true)
	cases := map[error]string{
		fmt.Errorf("connect: %w", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}): "connection refused",
		fmt.Errorf("x: %w", context.DeadlineExceeded):                                  "timed out",
		&net.DNSError{Err: "no such host", Name: "nope.invalid"}:                       "host not found",
		fmt.Errorf("send to h: %w", &HTTPStatusError{Code: 404}):                       "HTTP 404",
		fmt.Errorf("ssh x: %w: %w", remote.ErrAuthFailed, errors.New("ssh: unable")):   "authentication failed",
		errors.New("read host key: ssh: handshake failed: EOF"):                        "SSH handshake failed",
		errors.New("tls: first record does not look like a TLS handshake"):             "TLS error",
		errors.New("internal detail /var/lib/secret"):                                  "connection failed",
	}
	for err, want := range cases {
		if got := netErrorText(err); got != want {
			t.Errorf("netErrorText(%v) = %q, want %q", err, got, want)
		}
	}
	if got := netErrorText(fmt.Errorf("dial: %w", blocked)); !strings.Contains(got, "esa does not connect to 169.254.169.254") {
		t.Errorf("blocked: %q", got)
	}
}

// sshServer is an in-process SSH server with one host key.
func sshServer(t *testing.T) (host string, port int, key ssh.PublicKey) {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
		return nil, errors.New("no")
	}}
	cfg.AddHostKey(signer)
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
			go func() {
				_, _, _, _ = ssh.NewServerConn(c, cfg)
				c.Close()
			}()
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	port, _ = strconv.Atoi(p)
	return h, port, signer.PublicKey()
}

func TestConfirmHostKey(t *testing.T) {
	host, port, key := sshServer(t)
	s := &Service{allowPrivate: true, log: discardLog()}
	srv := store.Server{ID: 1, Host: host, Port: port}
	ctx := context.Background()
	if err := s.confirmHostKey(ctx, srv, key); err != nil {
		t.Fatalf("matching key: %v", err)
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	otherKey, _ := ssh.NewPublicKey(other.Public())
	var verr *ValidationError
	if err := s.confirmHostKey(ctx, srv, otherKey); !errors.As(err, &verr) || !strings.Contains(err.Error(), "is not this server's key") {
		t.Fatalf("different key: %v", err)
	}
	// Loopback is refused when private networks are off.
	s.allowPrivate = false
	if err := s.confirmHostKey(ctx, srv, key); err == nil || !strings.Contains(err.Error(), "ESA_ALLOW_PRIVATE_NETWORKS") {
		t.Fatalf("blocked address: %v", err)
	}
	// Nothing listening.
	s.allowPrivate = true
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	_, p, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	closed, _ := strconv.Atoi(p)
	start := time.Now()
	if err := s.confirmHostKey(ctx, store.Server{Host: "127.0.0.1", Port: closed}, key); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("unreachable: %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("refused connection should fail fast")
	}
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
