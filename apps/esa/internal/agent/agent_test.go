package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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
