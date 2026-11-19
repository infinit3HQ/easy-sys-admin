package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

func TestAgentRejectsUnsafeOriginsAndCredentials(t *testing.T) {
	for _, origin := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com/path", "https://example.com?token=x", "http://localhost:8080"} {
		if _, err := New(origin, "esa_agent_test"); err == nil {
			t.Fatal("accepted", origin)
		}
	}
	for _, origin := range []string{"https://example.com", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		if _, err := New(origin, "esa_agent_test"); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "token")
	os.WriteFile(path, []byte("esa_agent_test"), 0644)
	if _, err := ReadToken(path); err == nil {
		t.Fatal("world readable token")
	}
	os.Chmod(path, 0640)
	if _, err := ReadToken(path); err != nil {
		t.Fatal(err)
	}
	link := path + "-link"
	os.Symlink(path, link)
	if _, err := ReadToken(link); err == nil {
		t.Fatal("symlink token")
	}
}
func TestAgentNeverExecutesNetworkChosenPrograms(t *testing.T) {
	for _, action := range []string{"/bin/sh", "sh", "upgrade-exec", "uninstall", "status; touch /tmp/unsafe"} {
		res := Execute(context.Background(), Command{ID: 1, Action: action, ExpiresAt: time.Now().Add(time.Minute)})
		if res.Exit == 0 {
			t.Fatal(action)
		}
	}
	if validCommand(Command{ID: 1, Action: "status", ExpiresAt: time.Now().Add(-time.Second)}) {
		t.Fatal("expired command accepted")
	}
}

// An agent with a pin runs only commands signed for its server. What answers
// at its origin, including a proxy, cannot give it work.
func TestAgentWithPinRunsOnlySignedCommands(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	const serverID = 7
	expires := time.Now().Add(time.Minute)
	sign := func(server, id int64, action string) string {
		return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, proto.AgentCommandMessage(server, id, action, []string{}, expires.Unix())))
	}
	commands := []Command{
		{ID: 1, Action: "status", Args: []string{}, ExpiresAt: expires, ServerID: serverID, Signature: sign(serverID, 1, "status")},
		{ID: 2, Action: "reboot", Args: []string{}, ExpiresAt: expires, ServerID: serverID, Signature: ""},
		{ID: 3, Action: "reboot", Args: []string{}, ExpiresAt: expires, ServerID: serverID, Signature: sign(serverID, 3, "status")},
		{ID: 4, Action: "status", Args: []string{}, ExpiresAt: expires, ServerID: 8, Signature: sign(8, 4, "status")},
		// A signed command delivered a second time, or one with an older id
		// delivered after a newer one, is not run again.
		{ID: 1, Action: "status", Args: []string{}, ExpiresAt: expires, ServerID: serverID, Signature: sign(serverID, 1, "status")},
		{ID: 5, Action: "status", Args: []string{}, ExpiresAt: expires, ServerID: serverID, Signature: sign(serverID, 5, "status")},
		{ID: 4, Action: "status", Args: []string{}, ExpiresAt: expires, ServerID: serverID, Signature: sign(serverID, 4, "status")},
	}
	var (
		mu      sync.Mutex
		next    int
		results = map[int64]Result{}
		done    = make(chan struct{})
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/agent/poll":
			if next < len(commands) {
				_ = json.NewEncoder(w).Encode(map[string]any{"command": commands[next]})
				next++
				return
			}
			_, _ = w.Write([]byte("{}"))
		case "/api/v1/agent/result":
			var res Result
			_ = json.NewDecoder(r.Body).Decode(&res)
			results[res.ID] = res
			w.WriteHeader(http.StatusNoContent)
			if len(results) == 5 {
				close(done)
			}
		}
	}))
	defer srv.Close()
	client, err := New(srv.URL, "esa_agent_test")
	if err != nil {
		t.Fatal(err)
	}
	client.WithPin(proto.AgentPin{ServerID: serverID, Key: pub})
	var ran []int64
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go func() {
		<-done
		cancel()
	}()
	_ = client.Run(ctx, func(_ context.Context, c Command) Result {
		mu.Lock()
		ran = append(ran, c.ID)
		mu.Unlock()
		return Result{ID: c.ID, Output: `{"ok":true,"protocol":1}`}
	})
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 2 || ran[0] != 1 || ran[1] != 5 {
		t.Fatalf("ran commands %v, want 1 and 5 once each", ran)
	}
	for _, id := range []int64{2, 3, 4} {
		if res, ok := results[id]; !ok || res.Exit == 0 {
			t.Errorf("command %d: refusal was not reported: %+v", id, res)
		}
	}
}

// The mark survives a restart, so a command run before it is not run again.
func TestAgentKeepsReplayMarkAcrossRestarts(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "last-command-id"), []byte("5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := New("https://example.com", "esa_agent_test")
	if err != nil {
		t.Fatal(err)
	}
	c.WithPin(proto.AgentPin{ServerID: 7, Key: pub})
	if err := c.WithStateDir(dir); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Minute)
	cmd := func(id int64) Command {
		sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, proto.AgentCommandMessage(7, id, "status", []string{}, expires.Unix())))
		return Command{ID: id, Action: "status", Args: []string{}, ExpiresAt: expires, ServerID: 7, Signature: sig}
	}
	if c.commandTrusted(cmd(4)) {
		t.Fatal("a command older than the saved mark was accepted after a restart")
	}
	if !c.commandTrusted(cmd(6)) {
		t.Fatal("a newer signed command was refused")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "last-command-id"))
	if strings.TrimSpace(string(raw)) != "6" {
		t.Fatalf("saved mark = %q, want 6", raw)
	}
	if err := os.WriteFile(filepath.Join(dir, "last-command-id"), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.WithStateDir(dir); err == nil {
		t.Fatal("a damaged mark was accepted")
	}
}
