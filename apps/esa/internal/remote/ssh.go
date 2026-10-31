// Package remote talks to managed servers over SSH. Every connection pins
// the server's accepted host key; a different key aborts before any
// credential is sent or any command runs.
package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/netguard"
)

// HostKeyMismatchError means the server presented a different host key than
// the one accepted for it. Nothing was sent to the server.
type HostKeyMismatchError struct {
	Presented ssh.PublicKey
}

func (e *HostKeyMismatchError) Error() string {
	return "host key changed: server presented " + ssh.FingerprintSHA256(e.Presented) + "; refusing to connect until the new key is accepted"
}

// errProbeDone stops the handshake once the host key is captured.
var errProbeDone = errors.New("probe complete")

// Connection limits. The TCP connect is bounded separately from the SSH
// handshake, so a server that is down or filtered costs a poll worker only
// the connect timeout, while a slow but alive sshd still gets the full
// handshake time. Both are also bounded by the caller's context.
const (
	// DefaultConnectTimeout bounds the TCP connect unless the context sets
	// another limit with WithConnectTimeout (ESA_SSH_CONNECT_TIMEOUT).
	DefaultConnectTimeout = 5 * time.Second
	// handshakeTimeout bounds the SSH handshake and authentication.
	handshakeTimeout = 15 * time.Second
)

// ProbeHostKey connects far enough to read the host key, then disconnects
// before authenticating, so no password or key is offered to a host that
// has not been verified yet.
func ProbeHostKey(ctx context.Context, host string, port int) (ssh.PublicKey, error) {
	return probe(ctx, host, port, preferredHostKeyAlgos)
}

// ProbeHostKeyLike is ProbeHostKey restricted to the algorithm of want, so a
// server with several host keys presents the one of the same type.
func ProbeHostKeyLike(ctx context.Context, host string, port int, want ssh.PublicKey) (ssh.PublicKey, error) {
	return probe(ctx, host, port, []string{algoFor(want)})
}

func probe(ctx context.Context, host string, port int, algos []string) (ssh.PublicKey, error) {
	var presented ssh.PublicKey
	cfg := &ssh.ClientConfig{
		User: "esa-probe",
		Auth: []ssh.AuthMethod{},
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			presented = key
			return errProbeDone
		},
		HostKeyAlgorithms: algos,
		Timeout:           handshakeTimeout,
	}
	conn, err := dialTCP(ctx, host, port)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))
	_, _, _, err = ssh.NewClientConn(conn, net.JoinHostPort(host, strconv.Itoa(port)), cfg)
	if presented != nil {
		return presented, nil
	}
	if err == nil {
		err = errors.New("server did not present a host key")
	}
	return nil, fmt.Errorf("read host key: %w", err)
}

// preferredHostKeyAlgos asks for Ed25519 first so the fingerprint people
// see matches `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`.
var preferredHostKeyAlgos = []string{
	ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521,
	ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256,
}

// Target is everything needed to open an authenticated connection.
type Target struct {
	Host    string
	Port    int
	User    string
	Auth    []ssh.AuthMethod
	HostKey ssh.PublicKey // required
}

// Dial opens an SSH connection to t, pinning t.HostKey.
func Dial(ctx context.Context, t Target) (*ssh.Client, error) {
	if t.HostKey == nil {
		return nil, errors.New("remote: refusing to connect without an accepted host key")
	}
	want := t.HostKey.Marshal()
	cfg := &ssh.ClientConfig{
		User: t.User,
		Auth: t.Auth,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if !bytes.Equal(key.Marshal(), want) {
				return &HostKeyMismatchError{Presented: key}
			}
			return nil
		},
		HostKeyAlgorithms: []string{algoFor(t.HostKey)},
		Timeout:           handshakeTimeout,
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	conn, err := dialTCP(ctx, t.Host, t.Port)
	if err != nil {
		return nil, err
	}
	// Bound the handshake by the context as well as the timeout.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	_ = conn.SetDeadline(time.Now().Add(handshakeTimeout))
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	stop()
	if err != nil {
		conn.Close()
		var mismatch *HostKeyMismatchError
		if errors.As(err, &mismatch) {
			return nil, mismatch
		}
		if strings.Contains(err.Error(), "unable to authenticate") {
			return nil, fmt.Errorf("ssh %s@%s: %w: %w", t.User, addr, ErrAuthFailed, err)
		}
		return nil, fmt.Errorf("ssh %s@%s: %w", t.User, addr, err)
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

// algoFor returns the host key algorithm to negotiate for a pinned key. RSA
// keys use rsa-sha2-256, since ssh-rsa (SHA-1) is disabled on current servers.
func algoFor(k ssh.PublicKey) string {
	if k.Type() == ssh.KeyAlgoRSA {
		return ssh.KeyAlgoRSASHA256
	}
	return k.Type()
}

// ErrAuthFailed means the server rejected every offered credential.
var ErrAuthFailed = errors.New("ssh authentication failed")

type allowPrivateKey struct{}

// WithAllowPrivate sets whether SSH dials made with ctx may reach private
// and loopback addresses. Without it only the always-blocked addresses
// (metadata, link-local, multicast, unspecified) are refused.
func WithAllowPrivate(ctx context.Context, allow bool) context.Context {
	return context.WithValue(ctx, allowPrivateKey{}, allow)
}

func allowPrivate(ctx context.Context) bool {
	allow, ok := ctx.Value(allowPrivateKey{}).(bool)
	return !ok || allow
}

type connectTimeoutKey struct{}

// WithConnectTimeout sets the TCP connect timeout for SSH dials made with
// ctx. Zero or less keeps DefaultConnectTimeout.
func WithConnectTimeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, connectTimeoutKey{}, d)
}

func connectTimeout(ctx context.Context) time.Duration {
	if d, ok := ctx.Value(connectTimeoutKey{}).(time.Duration); ok && d > 0 {
		return d
	}
	return DefaultConnectTimeout
}

// dialTCP checks every resolved address against the address policy at
// connect time, so DNS rebinding cannot slip past an earlier check.
func dialTCP(ctx context.Context, host string, port int) (net.Conn, error) {
	d := netguard.Dialer(allowPrivate(ctx), net.Dialer{Timeout: connectTimeout(ctx), KeepAlive: 30 * time.Second})
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("connect to %s:%d: %w", host, port, err)
	}
	return conn, nil
}

// ExecResult is a finished remote command.
type ExecResult struct {
	Stdout []byte
	Stderr []byte
	Exit   int
}

// maxOutput caps captured output per command.
const maxOutput = 16 << 20

// Exec runs one command in a new session. Cancelling ctx closes the session.
func Exec(ctx context.Context, c *ssh.Client, cmd string, stdin io.Reader) (ExecResult, error) {
	sess, err := c.NewSession()
	if err != nil {
		return ExecResult{}, fmt.Errorf("open session: %w", err)
	}
	defer sess.Close()
	var out, errb limitedBuffer
	out.limit, errb.limit = maxOutput, 1<<20
	sess.Stdout, sess.Stderr = &out, &errb
	if stdin != nil {
		sess.Stdin = stdin
	}
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		return ExecResult{Stdout: out.Bytes(), Stderr: errb.Bytes()}, ctx.Err()
	case err = <-done:
	}
	res := ExecResult{Stdout: out.Bytes(), Stderr: errb.Bytes()}
	var exitErr *ssh.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.Exit = exitErr.ExitStatus()
	default:
		return res, fmt.Errorf("run command: %w", err)
	}
	return res, nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.Len(); room < len(p) {
		if room > 0 {
			b.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

// ShellQuote quotes s for a POSIX shell. Helper arguments are validated
// before they get here; quoting is a second line of defence.
func ShellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./,=:@%+", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Fingerprint is the SHA256 fingerprint of a public key.
func Fingerprint(k ssh.PublicKey) string { return ssh.FingerprintSHA256(k) }

// MarshalKey returns the key in authorized_keys format without a newline.
func MarshalKey(k ssh.PublicKey) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k)))
}

// ParseKey parses an authorized_keys-format key.
func ParseKey(s string) (ssh.PublicKey, error) {
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(s))
	return k, err
}
