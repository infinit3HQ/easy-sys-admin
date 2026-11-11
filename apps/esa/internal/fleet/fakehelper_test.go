package fleet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// fakeFleet runs in-process SSH servers that act like managed servers with
// esa-helper installed. Each fake server has its own listener on
// 127.0.0.1:0 and its own ed25519 host key.
type fakeFleet struct {
	t           *testing.T
	statusDelay time.Duration
	statusCalls atomic.Int64

	keyMu     sync.Mutex
	clientKey []byte // the account public key the servers accept

	mu      sync.Mutex
	closers []io.Closer
	closed  bool
}

// fakeHost is one fake server: kind is healthy, silent, or closed.
type fakeHost struct {
	Kind    string
	Host    string
	Port    int
	HostKey ssh.PublicKey
}

func newFakeFleet(t *testing.T, statusDelay time.Duration) *fakeFleet {
	f := &fakeFleet{t: t, statusDelay: statusDelay}
	t.Cleanup(f.Close)
	return f
}

// accept sets the client key the fake servers let in.
func (f *fakeFleet) accept(k ssh.PublicKey) {
	f.keyMu.Lock()
	f.clientKey = k.Marshal()
	f.keyMu.Unlock()
}

func (f *fakeFleet) track(c io.Closer) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		c.Close()
		return false
	}
	f.closers = append(f.closers, c)
	return true
}

// Close stops every listener and connection.
func (f *fakeFleet) Close() {
	f.mu.Lock()
	cs := f.closers
	f.closers, f.closed = nil, true
	f.mu.Unlock()
	for _, c := range cs {
		c.Close()
	}
}

func (f *fakeFleet) listen() (net.Listener, string, int) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		f.t.Fatal(err)
	}
	f.track(ln)
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(p)
	return ln, h, port
}

func newHostKey(t *testing.T) ssh.Signer {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

// healthy starts a server that answers esa-helper status after statusDelay.
func (f *fakeFleet) healthy() fakeHost {
	signer := newHostKey(f.t)
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		f.keyMu.Lock()
		ok := bytes.Equal(k.Marshal(), f.clientKey)
		f.keyMu.Unlock()
		if !ok {
			return nil, errUnknownKey
		}
		return &ssh.Permissions{}, nil
	}}
	cfg.AddHostKey(signer)
	ln, h, port := f.listen()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			if !f.track(c) {
				return
			}
			go f.serve(c, cfg)
		}
	}()
	return fakeHost{Kind: "healthy", Host: h, Port: port, HostKey: signer.PublicKey()}
}

// silent starts a listener that accepts TCP and never sends an SSH banner,
// like a hung sshd. Clients wait for the handshake timeout.
func (f *fakeFleet) silent() fakeHost {
	ln, h, port := f.listen()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			if !f.track(c) {
				return
			}
			go func() { _, _ = io.Copy(io.Discard, c); c.Close() }()
		}
	}()
	return fakeHost{Kind: "silent", Host: h, Port: port, HostKey: newHostKey(f.t).PublicKey()}
}

// closedPort returns an address nothing listens on, so connects are refused.
func (f *fakeFleet) closedPort() fakeHost {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		f.t.Fatal(err)
	}
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	port, _ := strconv.Atoi(p)
	return fakeHost{Kind: "closed", Host: h, Port: port, HostKey: newHostKey(f.t).PublicKey()}
}

var errUnknownKey = errors.New("unknown key")

func (f *fakeFleet) serve(c net.Conn, cfg *ssh.ServerConfig) {
	defer c.Close()
	sc, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		ch, creqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go f.session(ch, creqs)
	}
}

const (
	statusCommand  = "sudo -n " + proto.HelperPath + " status"
	updatesCommand = "sudo -n " + proto.HelperPath + " updates"
)

func (f *fakeFleet) session(ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		if req.Type != "exec" {
			_ = req.Reply(false, nil)
			continue
		}
		var p struct{ Command string }
		if err := ssh.Unmarshal(req.Payload, &p); err != nil {
			_ = req.Reply(false, nil)
			return
		}
		_ = req.Reply(true, nil)
		exit := uint32(0)
		switch p.Command {
		case statusCommand:
			f.statusCalls.Add(1)
			time.Sleep(f.statusDelay)
			_, _ = ch.Write(fakeStatusEnvelope)
		case updatesCommand:
			_, _ = ch.Write(envelope(proto.PendingUpdates{}))
		default:
			_, _ = ch.Stderr().Write([]byte("unknown action\n"))
			exit = 1
		}
		_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{exit}))
		return
	}
}

func envelope(v any) []byte {
	data, _ := json.Marshal(v)
	b, _ := json.Marshal(proto.Envelope{OK: true, Protocol: proto.Version, Data: data})
	return b
}

// fakeStatusEnvelope is what `esa-helper status` prints on a small Ubuntu
// server running Docker.
var fakeStatusEnvelope = envelope(proto.Status{
	CollectedAt: time.Now().UTC(), Hostname: "fake", OS: "Ubuntu 24.04.2 LTS", Kernel: "6.8.0-60-generic", Arch: "amd64",
	BootTime: time.Now().Add(-72 * time.Hour).UTC(), UptimeSeconds: 72 * 3600,
	CPUCount: 4, CPUPercent: 12.4, Load1: 0.42, Load5: 0.35, Load15: 0.3,
	MemTotal: 8 << 30, MemUsed: 3 << 30, MemAvailable: 5 << 30, SwapTotal: 2 << 30, SwapUsed: 64 << 20,
	Disks: []proto.Disk{
		{Mount: "/", Device: "/dev/sda1", FSType: "ext4", Total: 80 << 30, Used: 31 << 30, Avail: 45 << 30, InodesTotal: 5_242_880, InodesUsed: 412_000},
		{Mount: "/boot", Device: "/dev/sda16", FSType: "ext4", Total: 900 << 20, Used: 150 << 20, Avail: 690 << 20, InodesTotal: 58_000, InodesUsed: 600},
	},
	Net:        []proto.NetIf{{Name: "eth0", RxBytes: 912_345_678, TxBytes: 123_456_789}, {Name: "docker0", RxBytes: 1_234_567, TxBytes: 7_654_321}},
	TimeSynced: true, FailedUnits: []string{},
	Docker: &proto.DockerSummary{Version: "28.1.1", Running: 6, Exited: 1, Total: 7},
})

// insertFakeServers adds ready servers for hosts to the account, with
// pinned host keys and a recent successful poll. missed gives each
// server's starting missed-poll count.
func insertFakeServers(t *testing.T, s *Service, accountID int64, hosts []fakeHost, missed func(i int) int, lastAttempt func(i int) time.Time) []int64 {
	t.Helper()
	now := time.Now().UTC()
	servers := make([]store.Server, len(hosts))
	for i, h := range hosts {
		servers[i] = store.Server{AccountID: accountID, Name: h.Kind + "-" + strconv.Itoa(i), Host: h.Host, Port: h.Port, Username: "esa",
			Enrollment: "ready", HostKey: remote.MarshalKey(h.HostKey), HostKeyFingerprint: remote.Fingerprint(h.HostKey),
			ProtectedContainers: store.JSON("[]"), CreatedAt: now, UpdatedAt: now}
	}
	if err := s.db.Omit("Account", "Group").CreateInBatches(&servers, 500).Error; err != nil {
		t.Fatal(err)
	}
	seen := now.Add(-time.Minute)
	states := make([]store.ServerState, len(hosts))
	ids := make([]int64, len(hosts))
	for i, srv := range servers {
		ids[i] = srv.ID
		at := lastAttempt(i)
		states[i] = store.ServerState{ServerID: srv.ID, Status: string(StatusOK), Reasons: store.JSON("[]"),
			LastSeenAt: &seen, LastAttemptAt: &at, UpdatesAt: &now, Updates: store.MustJSON(proto.PendingUpdates{}),
			ExposureAt: &now,
			MissedPolls: missed(i), UpdatedAt: now}
	}
	if err := s.db.Omit("Server").CreateInBatches(&states, 500).Error; err != nil {
		t.Fatal(err)
	}
	return ids
}

// acceptAccountKey makes the fake fleet accept the account's SSH key.
func acceptAccountKey(t *testing.T, s *Service, f *fakeFleet, accountID int64) {
	t.Helper()
	k, err := s.accountKey(context.Background(), accountID)
	if err != nil {
		t.Fatal(err)
	}
	f.accept(k.signer.PublicKey())
}
