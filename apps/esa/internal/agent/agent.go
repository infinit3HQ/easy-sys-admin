// Package agent carries fixed esa-helper requests over an outbound connection.
// It cannot run shell commands or accept an executable path from the network.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

const maxOutput = 4 << 20

type Command struct {
	ID        int64     `json:"id"`
	Action    string    `json:"action"`
	Args      []string  `json:"args"`
	ExpiresAt time.Time `json:"expiresAt"`
	// ServerID and Signature are checked against the pin when there is one.
	ServerID  int64  `json:"serverId"`
	Signature string `json:"signature"`
}
type Result struct {
	ID     int64  `json:"id"`
	Output string `json:"output"`
	Exit   int    `json:"exit"`
}
type Client struct {
	base, token string
	http        *http.Client
	// pin, when set, makes the agent run only commands signed by the control
	// plane for this server. Without one, the agent trusts whatever answers
	// at its origin, which includes any proxy in front of the control plane.
	pin *proto.AgentPin
	// lastID is the highest command id this process has accepted. Ids rise
	// with every command the control plane queues, so a signed command that
	// something replays or holds back and delivers late is refused.
	lastID int64
}

// WithPin makes the client check every command against pin and returns it.
func (c *Client) WithPin(pin proto.AgentPin) *Client {
	c.pin = &pin
	return c
}

// commandTrusted reports whether cmd may run. Without a pin every command
// passes this check, as before. With one, the command must carry a valid
// signature for this server and a higher id than any accepted before. The
// mark lives in memory: a restart forgets it, and the signature's expiry (at
// most ten minutes) bounds what could be replayed after one.
func (c *Client) commandTrusted(cmd Command) bool {
	if c.pin == nil {
		return true
	}
	if cmd.ID <= c.lastID {
		return false
	}
	if !c.pin.VerifyAgentCommand(cmd.ServerID, cmd.ID, cmd.Action, cmd.Args, cmd.ExpiresAt.Unix(), cmd.Signature) {
		return false
	}
	c.lastID = cmd.ID
	return true
}

func New(rawURL, token string) (*Client, error) {
	u, e := url.Parse(rawURL)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return nil, errors.New("ESA_AGENT_URL must be an HTTPS origin")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && (u.Scheme != "http" || ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("ESA_AGENT_URL needs HTTPS; HTTP is allowed only on a numeric loopback address")
	}
	if !strings.HasPrefix(token, "esa_agent_") || len(token) > 200 || strings.ContainsAny(token, "\r\n\t ") {
		return nil, errors.New("invalid agent credential")
	}
	return &Client{base: strings.TrimRight(rawURL, "/"), token: token, http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("agent redirects are refused") }}}, nil
}

// ReadToken rejects symlinks, special files, and credentials readable by others.
func ReadToken(path string) (string, error) {
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0007 != 0 || st.Mode().Perm()&0020 != 0 || st.Size() > 200 {
		return "", errors.New("agent credential needs a regular file with mode 0600 or 0640")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		return "", errors.New("cannot read agent credential")
	}
	return strings.TrimSpace(string(raw)), nil
}
func (c *Client) request(ctx context.Context, path string, body, out any) error {
	raw, e := json.Marshal(body)
	if e != nil {
		return e
	}
	r, e := http.NewRequestWithContext(ctx, "POST", c.base+"/api/v1/agent/"+path, bytes.NewReader(raw))
	if e != nil {
		return e
	}
	r.Header.Set("X-Esa-Agent-Token", c.token)
	r.Header.Set("Content-Type", "application/json")
	res, e := c.http.Do(r)
	if e != nil {
		return errors.New("outbound request failed")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return errors.New("control plane refused outbound request")
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
	}
	return nil
}
func validCommand(c Command) bool {
	if c.ID <= 0 || !slices.Contains(proto.Actions, c.Action) || c.Action == "upgrade-exec" || c.Action == "uninstall" || len(c.Args) > 256 || time.Until(c.ExpiresAt) <= 0 || time.Until(c.ExpiresAt) > 10*time.Minute+time.Minute {
		return false
	}
	for _, arg := range c.Args {
		if len(arg) > 65536 || strings.ContainsRune(arg, 0) {
			return false
		}
	}
	return true
}
func failure(id int64, message string) Result {
	raw, _ := json.Marshal(proto.Envelope{OK: false, Protocol: proto.Version, Code: proto.CodeRefused, Error: message})
	return Result{id, string(raw), 1}
}

type bounded struct {
	bytes.Buffer
	tooLarge bool
}

func (b *bounded) Write(p []byte) (int, error) {
	n := len(p)
	room := maxOutput - b.Len()
	if room < len(p) {
		b.tooLarge = true
		if room > 0 {
			b.Buffer.Write(p[:room])
		}
		return n, nil
	}
	return b.Buffer.Write(p)
}

// Execute uses a fixed sudo executable and fixed helper path. The helper owns
// argument validation, privilege boundaries, and all host safety rules.
func Execute(ctx context.Context, c Command) Result {
	if !validCommand(c) {
		return failure(c.ID, "invalid or expired outbound helper command")
	}
	if c.Action == "upgrade-start" {
		version := exec.CommandContext(ctx, "/usr/bin/sudo", "-n", proto.HelperPath, "version")
		version.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
		var output bounded
		version.Stdout = &output
		if version.Run() != nil || output.tooLarge {
			return failure(c.ID, "could not verify helper idempotency")
		}
		var envelope proto.Envelope
		var info proto.VersionInfo
		if json.Unmarshal(output.Bytes(), &envelope) != nil || !envelope.OK || json.Unmarshal(envelope.Data, &info) != nil || info.Protocol != proto.Version || !slices.Contains(info.Capabilities, proto.FeatureUpgradeIdempotent) {
			return failure(c.ID, "outbound upgrades require a helper with upgrade-idempotent support")
		}
	}
	ctx, cancel := context.WithDeadline(ctx, c.ExpiresAt)
	defer cancel()
	args := append([]string{"-n", proto.HelperPath, c.Action}, c.Args...)
	cmd := exec.CommandContext(ctx, "/usr/bin/sudo", args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	var output bounded
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if ctx.Err() != nil {
		return failure(c.ID, "outbound helper command exceeded its deadline")
	}
	if output.tooLarge {
		return failure(c.ID, "helper output exceeded the outbound limit")
	}
	exit := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		} else {
			return failure(c.ID, "could not start the installed helper")
		}
	}
	var envelope proto.Envelope
	if json.Unmarshal(output.Bytes(), &envelope) != nil {
		return failure(c.ID, "installed helper returned no valid envelope")
	}
	return Result{c.ID, output.String(), exit}
}

// Run never repeats a claimed command. A lost result is retried until expiry;
// a restart leaves it unknown for the control plane to follow or interrupt.
func (c *Client) Run(ctx context.Context, execute func(context.Context, Command) Result) error {
	wait := func(d time.Duration) bool {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
			return true
		}
	}
	backoff := 2 * time.Second
	for ctx.Err() == nil {
		var poll struct {
			Command *Command `json:"command"`
		}
		if err := c.request(ctx, "poll", nil, &poll); err != nil {
			if !wait(backoff) {
				break
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = 2 * time.Second
		if poll.Command == nil {
			if !wait(2 * time.Second) {
				break
			}
			continue
		}
		var result Result
		if c.commandTrusted(*poll.Command) {
			result = execute(ctx, *poll.Command)
		} else {
			// Never run it. Report the refusal so the control plane does not
			// wait for a result until the command expires.
			result = failure(poll.Command.ID, "the command is not signed for this server")
		}
		for time.Now().Before(poll.Command.ExpiresAt) {
			if c.request(ctx, "result", result, nil) == nil {
				break
			}
			if !wait(2 * time.Second) {
				return ctx.Err()
			}
		}
	}
	return ctx.Err()
}
