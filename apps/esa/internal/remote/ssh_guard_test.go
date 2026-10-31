package remote

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/netguard"
)

// With private networks off, loopback is refused before connecting; the
// metadata address is refused either way.
func TestDialAddressPolicy(t *testing.T) {
	ts := startServer(t)
	host, port := ts.hostPort(t)
	strict := WithAllowPrivate(context.Background(), false)
	if _, err := ProbeHostKey(strict, host, port); !netguard.IsBlocked(err) {
		t.Fatalf("probe loopback with private networks off: %v", err)
	}
	if _, err := Dial(strict, Target{Host: host, Port: port, User: "x", HostKey: ts.hostKey}); !netguard.IsBlocked(err) {
		t.Fatalf("dial loopback with private networks off: %v", err)
	}
	if _, err := ProbeHostKey(context.Background(), "169.254.169.254", 22); !netguard.IsBlocked(err) {
		t.Fatalf("probe metadata: %v", err)
	}
	if _, err := ProbeHostKey(WithAllowPrivate(context.Background(), true), host, port); err != nil {
		t.Fatalf("probe loopback with private networks on: %v", err)
	}
}

func TestDialMarksAuthFailure(t *testing.T) {
	ts := startServer(t)
	host, port := ts.hostPort(t)
	_, err := Dial(context.Background(), Target{Host: host, Port: port, User: "root", HostKey: ts.hostKey,
		Auth: []ssh.AuthMethod{ssh.Password("wrong")}})
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("want ErrAuthFailed, got %v", err)
	}
}

func TestProbeHostKeyLike(t *testing.T) {
	ts := startServer(t)
	host, port := ts.hostPort(t)
	k, err := ProbeHostKeyLike(context.Background(), host, port, ts.hostKey)
	if err != nil || ssh.FingerprintSHA256(k) != ssh.FingerprintSHA256(ts.hostKey) {
		t.Fatalf("probe like ed25519: %v", err)
	}
	// The server has no ECDSA key, so asking for one finds nothing.
	ecdsaKey, _ := ParseKey("ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBEmKSENjQEezOmxkZMy7opKgwFB9nkt5YRrYMjNuG5N87uRgg6CLrbo5wAdT/y6v0mKV0U2w0WZ2YB/++Tpockg=")
	if ecdsaKey == nil {
		t.Skip("could not parse test key")
	}
	if _, err := ProbeHostKeyLike(context.Background(), host, port, ecdsaKey); err == nil {
		t.Fatal("probe for a key type the server lacks must fail")
	}
}
