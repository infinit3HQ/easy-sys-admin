package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

const (
	keepaliveEvery = 30 * time.Second
	idleClose      = 10 * time.Minute
)

// Endpoint identifies a server and how to log in to it as the esa user.
type Endpoint struct {
	ServerID int64
	Host     string
	Port     int
	User     string
	HostKey  ssh.PublicKey
	Signer   ssh.Signer
}

func (e Endpoint) key() string {
	return fmt.Sprintf("%d|%s|%d|%s|%s", e.ServerID, e.Host, e.Port, e.User, ssh.FingerprintSHA256(e.HostKey))
}

// Pool keeps one SSH connection per server and opens a new session for
// every command. Idle connections close after ten minutes.
type Pool struct {
	mu    sync.Mutex
	conns map[int64]*pooled
}

type pooled struct {
	key      string
	client   *ssh.Client
	lastUsed time.Time
	done     chan struct{}
}

// NewPool returns an empty pool.
func NewPool() *Pool { return &Pool{conns: map[int64]*pooled{}} }

// Close closes every connection.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, c := range p.conns {
		close(c.done)
		_ = c.client.Close()
		delete(p.conns, id)
	}
}

// Len is the number of open connections.
func (p *Pool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.conns)
}

// Forget closes the connection to one server, for example after its
// address or host key changed.
func (p *Pool) Forget(serverID int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.conns[serverID]; ok {
		close(c.done)
		_ = c.client.Close()
		delete(p.conns, serverID)
	}
}

func (p *Pool) get(ctx context.Context, e Endpoint) (*ssh.Client, error) {
	p.mu.Lock()
	if c, ok := p.conns[e.ServerID]; ok {
		if c.key == e.key() {
			c.lastUsed = time.Now()
			p.mu.Unlock()
			return c.client, nil
		}
		close(c.done)
		_ = c.client.Close()
		delete(p.conns, e.ServerID)
	}
	p.mu.Unlock()

	client, err := Dial(ctx, Target{Host: e.Host, Port: e.Port, User: e.User, HostKey: e.HostKey, Auth: []ssh.AuthMethod{ssh.PublicKeys(e.Signer)}})
	if err != nil {
		return nil, err
	}
	c := &pooled{key: e.key(), client: client, lastUsed: time.Now(), done: make(chan struct{})}

	p.mu.Lock()
	if existing, ok := p.conns[e.ServerID]; ok && existing.key == c.key {
		// Another goroutine connected first; use its connection.
		p.mu.Unlock()
		_ = client.Close()
		return existing.client, nil
	}
	p.conns[e.ServerID] = c
	p.mu.Unlock()
	go p.keepalive(e.ServerID, c)
	return client, nil
}

// keepalive pings the server and drops the connection when it stops
// answering or sits idle.
func (p *Pool) keepalive(serverID int64, c *pooled) {
	t := time.NewTicker(keepaliveEvery)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
		}
		p.mu.Lock()
		idle := time.Since(c.lastUsed) > idleClose
		p.mu.Unlock()
		ok := !idle
		if ok {
			_, _, err := c.client.SendRequest("keepalive@openssh.com", true, nil)
			ok = err == nil
		}
		if !ok {
			p.mu.Lock()
			if p.conns[serverID] == c {
				delete(p.conns, serverID)
				close(c.done)
			}
			p.mu.Unlock()
			_ = c.client.Close()
			return
		}
	}
}

// HelperError is a failure reported by esa-helper itself.
type HelperError struct {
	Code    string
	Message string
}

func (e *HelperError) Error() string { return e.Message }

// IsRefused reports whether err is a helper safety refusal.
func IsRefused(err error) bool {
	var he *HelperError
	return errors.As(err, &he) && he.Code == proto.CodeRefused
}

// Call runs `sudo -n esa-helper <action> <args...>` on the server and
// decodes the response data into out (which may be nil).
func (p *Pool) Call(ctx context.Context, e Endpoint, out any, action string, args ...string) error {
	client, err := p.get(ctx, e)
	if err != nil {
		return err
	}
	res, err := Exec(ctx, client, helperCommand(action, args...), nil)
	if err != nil {
		// A broken connection: drop it so the next call reconnects.
		if ctx.Err() == nil {
			p.Forget(e.ServerID)
		}
		return err
	}
	return decodeEnvelope(res, out)
}

func helperCommand(action string, args ...string) string {
	parts := []string{"sudo", "-n", proto.HelperPath, ShellQuote(action)}
	for _, a := range args {
		parts = append(parts, ShellQuote(a))
	}
	return strings.Join(parts, " ")
}

func decodeEnvelope(res ExecResult, out any) error {
	var env proto.Envelope
	if err := json.Unmarshal(res.Stdout, &env); err != nil {
		msg := strings.TrimSpace(string(res.Stderr))
		switch {
		case strings.Contains(msg, "a password is required"), strings.Contains(msg, "not allowed to execute"):
			return &HelperError{Code: proto.CodeFailed, Message: "sudo refused to run esa-helper; reinstall the helper on this server"}
		case strings.Contains(msg, "command not found"), strings.Contains(msg, "No such file"):
			return &HelperError{Code: proto.CodeFailed, Message: "esa-helper is not installed on this server"}
		case msg == "":
			msg = fmt.Sprintf("esa-helper exited %d without output", res.Exit)
		}
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return &HelperError{Code: proto.CodeFailed, Message: msg}
	}
	if !env.OK {
		return &HelperError{Code: env.Code, Message: env.Error}
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}
