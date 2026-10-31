package fleet

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func wantConflict(t *testing.T, what string, err error) {
	t.Helper()
	var c *ConflictError
	if !errors.As(err, &c) {
		t.Fatalf("%s: want ConflictError, got %v", what, err)
	}
}

func TestAccountCaps(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "caps")

	for i := range maxChannels {
		if _, err := s.CreateChannel(ctx, a, "ch-"+strconv.Itoa(i), "ntfy", ChannelConfig{URL: "https://ntfy.sh/x"}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.CreateChannel(ctx, a, "one-too-many", "ntfy", ChannelConfig{URL: "https://ntfy.sh/x"})
	wantConflict(t, "channel over the cap", err)

	certs := make([]store.CertTarget, maxCerts)
	for i := range certs {
		certs[i] = store.CertTarget{AccountID: a.AccountID, Hostname: "h" + strconv.Itoa(i) + ".example.com", Port: 443, CreatedAt: time.Now()}
	}
	if err := s.db.Omit("Account").Create(&certs).Error; err != nil {
		t.Fatal(err)
	}
	_, err = s.AddCert(ctx, a, "one-more.example.com", 443)
	wantConflict(t, "cert over the cap", err)

	servers := make([]store.Server, maxServers)
	for i := range servers {
		servers[i] = store.Server{AccountID: a.AccountID, Name: "s" + strconv.Itoa(i), Host: "192.0.2.1", Port: 22, Username: "esa",
			Enrollment: "pending", ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	}
	if err := s.db.Omit("Account", "Group").Create(&servers).Error; err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateEnrollment(ctx, a, EnrollInput{Name: "one-more", Host: "192.0.2.2"})
	wantConflict(t, "server over the cap", err)

	// Another account is not affected.
	b := signIn(t, s, "caps-other")
	if _, err := s.CreateChannel(ctx, b, "mine", "ntfy", ChannelConfig{URL: "https://ntfy.sh/y"}); err != nil {
		t.Fatalf("other account: %v", err)
	}
}

func TestEnrollmentTokensAndHostKey(t *testing.T) {
	s := newTestService(t)
	s.allowPrivate = true // the test SSH server listens on loopback
	ctx := context.Background()
	a := signIn(t, s, "enroll")
	host, port, key := sshServer(t)

	e1, err := s.CreateEnrollment(ctx, a, EnrollInput{Name: "web-1", Host: host, Port: port})
	if err != nil {
		t.Fatal(err)
	}
	tok1 := e1.ScriptURL[strings.LastIndex(e1.ScriptURL, "/")+1:]
	e2, err := s.NewEnrollToken(ctx, a, e1.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	tok2 := e2.ScriptURL[strings.LastIndex(e2.ScriptURL, "/")+1:]
	if _, err := s.EnrollScript(ctx, tok1); !errors.Is(err, ErrEnrollTokenInvalid) {
		t.Fatalf("an older enroll command must stop working: %v", err)
	}
	script, err := s.EnrollScript(ctx, tok2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(script, "\nesa_enroll\n") {
		t.Fatal("the script must end with the call to its function")
	}

	// A key the host does not present is refused, and the token stays usable.
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	other, _ := ssh.NewPublicKey(priv.Public())
	var verr *ValidationError
	if err := s.CompleteEnrollment(ctx, tok2, remote.MarshalKey(other)); !errors.As(err, &verr) {
		t.Fatalf("wrong host key: %v", err)
	}
	if err := s.CompleteEnrollment(ctx, tok2, remote.MarshalKey(key)); err != nil {
		t.Fatalf("matching host key: %v", err)
	}
	s.Wait()
	srv, err := s.loadServer(ctx, a, e1.Server.ID)
	if err != nil || srv.Enrollment != "ready" || srv.HostKeyFingerprint != remote.Fingerprint(key) {
		t.Fatalf("server after enroll: %+v %v", srv, err)
	}
	if err := s.CompleteEnrollment(ctx, tok2, remote.MarshalKey(key)); !errors.Is(err, ErrEnrollTokenInvalid) {
		t.Fatalf("token reuse: %v", err)
	}
	_, err = s.NewEnrollToken(ctx, a, srv.ID)
	wantConflict(t, "new token for an enrolled server", err)

	// Plain http to a non-loopback host is refused.
	s.publicURL, _ = url.Parse("http://esa.example.com")
	_, err = s.CreateEnrollment(ctx, a, EnrollInput{Name: "web-2", Host: host, Port: port})
	wantConflict(t, "enroll over http", err)
}

func TestStartRunValidatesHold(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "hold")
	srv := addTestServer(t, s, a, nil, "h-1")
	_, err := s.StartRun(ctx, a, RunRequest{Kind: RunUpdate, ServerIDs: []int64{srv.ID},
		Options: &RunOptions{UpdateMode: "security", Hold: []string{"openssl", "bad;rm -rf /"}}})
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Field != "hold" {
		t.Fatalf("want a hold validation error, got %v", err)
	}
	s.Wait()
}

// hangingNotifier blocks every send until its context ends.
type hangingNotifier struct{ calls chan struct{} }

func (h *hangingNotifier) Send(ctx context.Context, _ store.NotifyChannel, _ []byte, _ Message) error {
	h.calls <- struct{}{}
	<-ctx.Done()
	return ctx.Err()
}

// A channel that never answers must not hold up alert evaluation (and so
// polling); the test send still runs synchronously and reports a category.
func TestNotificationsOffThePollPath(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "notify")
	ch, err := s.CreateChannel(ctx, a, "slow", "webhook", ChannelConfig{URL: "https://hook.example.com/x"})
	if err != nil {
		t.Fatal(err)
	}
	h := &hangingNotifier{calls: make(chan struct{}, 10)}
	s.notifier = h
	srv := addTestServer(t, s, a, nil, "n-1")
	st := setSnapshot(t, s, srv, 95)

	start := time.Now()
	s.evaluateAlerts(ctx, srv, &st)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("evaluateAlerts waited %s for a hanging channel", d)
	}
	select {
	case <-h.calls:
	case <-time.After(5 * time.Second):
		t.Fatal("the queued notification was never sent")
	}

	tctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	err = s.TestChannel(tctx, a, ch.ID)
	var un *UnavailableError
	if !errors.As(err, &un) || un.Message != "timed out" {
		t.Fatalf("TestChannel: %v", err)
	}
}
