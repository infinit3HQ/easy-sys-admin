package hostops

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// recordingRunner records every command and returns canned output.
type recordingRunner struct {
	mu   sync.Mutex
	cmds []string
	out  map[string]string // command prefix -> stdout
}

func (r *recordingRunner) Run(_ context.Context, _ time.Duration, name string, args ...string) (Result, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	r.mu.Lock()
	r.cmds = append(r.cmds, cmd)
	r.mu.Unlock()
	for prefix, out := range r.out {
		if strings.HasPrefix(cmd, prefix) {
			return Result{Stdout: []byte(out)}, nil
		}
	}
	return Result{}, nil
}

// fakeDocker serves the Docker Engine API on a Unix socket and records requests.
type fakeDocker struct {
	mu       sync.Mutex
	requests []string
}

func startFakeDocker(t *testing.T) (*DockerClient, *fakeDocker) {
	t.Helper()
	dir, err := os.MkdirTemp("", "esa-docker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	fd := &fakeDocker{}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fd.mu.Lock()
		fd.requests = append(fd.requests, r.Method+" "+r.URL.RequestURI())
		fd.mu.Unlock()
		switch {
		case r.URL.Path == "/system/df":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"BuildCache": []map[string]any{{"Size": 1000, "InUse": false}, {"Size": 500, "InUse": false, "Shared": true}},
			})
		case r.URL.Path == "/build/prune":
			_ = json.NewEncoder(w).Encode(map[string]any{"SpaceReclaimed": 1000})
		case r.URL.Path == "/containers/json":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"Id": "aaaaaaaaaaaa1111", "Names": []string{"/traefik"}, "State": "running", "Labels": map[string]string{}},
				{"Id": "bbbbbbbbbbbb2222", "Names": []string{"/app"}, "State": "running", "Labels": map[string]string{}},
			})
		case strings.HasSuffix(r.URL.Path, "/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "x", "State": map[string]any{"Status": "running"}})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return NewDockerClient(sock), fd
}

func (f *fakeDocker) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

// The draft's "rules with no switch": cleanups never prune images, volumes,
// or the whole system, and never restart Docker or containers.
func TestCleanupNeverIssuesForbiddenCommands(t *testing.T) {
	docker, fd := startFakeDocker(t)
	run := &recordingRunner{out: map[string]string{"journalctl --disk-usage": "Archived and active journals take up 1.1G in the file system."}}
	h := &Host{Run: run, Docker: docker, Root: t.TempDir()}

	rep, err := h.Clean(context.Background(), CleanOptions{Apply: true, JournalSize: "200M"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Items) != 3 {
		t.Fatalf("want 3 cleanup results, got %d", len(rep.Items))
	}

	for _, req := range fd.all() {
		for _, bad := range []string{"/images/prune", "/volumes", "/containers/prune", "/system/prune", "/networks/prune", "/restart", "/stop", "/kill"} {
			if strings.Contains(req, bad) {
				t.Errorf("cleanup sent forbidden Docker request %q", req)
			}
		}
		if strings.HasPrefix(req, "POST /build/prune") && strings.Contains(req, "all") {
			t.Errorf("build cache prune must not use all=true: %q", req)
		}
	}
	for _, cmd := range run.cmds {
		for _, bad := range []string{"docker system prune", "image prune", "--volumes", "systemctl restart docker", "systemctl stop", "rm -rf /"} {
			if strings.Contains(cmd, bad) {
				t.Errorf("cleanup ran forbidden command %q", cmd)
			}
		}
	}
	if got := rep.Items[0]; got.ID != "buildcache" || got.Status != "cleaned" || got.Freed != 1000 {
		t.Errorf("build cache result = %+v", got)
	}
}

func TestBuildCacheSkippedWhileBuildRuns(t *testing.T) {
	docker, fd := startFakeDocker(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proc", "4242"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "proc", "4242", "cmdline"), []byte("docker\x00buildx\x00build\x00."), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &Host{Run: &recordingRunner{}, Docker: docker, Root: root}
	rep, err := h.Clean(context.Background(), CleanOptions{Apply: true, Only: []string{"buildcache"}})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Items[0].Status != "skipped" {
		t.Fatalf("want skipped while a build runs, got %+v", rep.Items[0])
	}
	for _, req := range fd.all() {
		if strings.Contains(req, "/build/prune") {
			t.Fatal("pruned build cache during a build")
		}
	}
}

func TestIsBuildCommand(t *testing.T) {
	cases := map[string]bool{
		"docker build .":               true,
		"docker buildx build --push .": true,
		"docker compose build web":     true,
		"buildctl build --frontend x":  true,
		"docker ps":                    false,
		"docker compose up -d":         false,
		"/usr/bin/dockerd -H fd://":    false,
		"vim build.sh":                 false,
	}
	for cmd, want := range cases {
		if got := isBuildCommand(strings.Fields(cmd)); got != want {
			t.Errorf("isBuildCommand(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestVSCodeKeepsNewestAndInUseCopies(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "root/.vscode-server/bin")
	mk := func(name string, age time.Duration) string {
		p := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Join(p, "node_modules"), 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(p, "node"), make([]byte, 100), 0o755)
		mt := time.Now().Add(-age)
		_ = os.Chtimes(p, mt, mt)
		return p
	}
	newest := mk(strings.Repeat("a", 40), time.Hour)
	inUse := mk(strings.Repeat("b", 40), 48*time.Hour)
	old := mk(strings.Repeat("c", 40), 72*time.Hour)
	freshStaging := mk(strings.Repeat("d", 40)+".staging", time.Minute)
	if err := os.WriteFile(filepath.Join(root, "etc-passwd"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	h := &Host{Run: &recordingRunner{}, Root: root}
	got := h.staleVSCodeCopies("/root", ".vscode-server/bin", []string{strings.TrimPrefix(inUse, root) + "/node"})
	if len(got) != 1 || got[0].path != strings.TrimPrefix(old, root) {
		t.Fatalf("want only the old, unused copy removed, got %+v", got)
	}
	_ = newest
	_ = freshStaging
}

func TestProtectedUnitsCannotBeStopped(t *testing.T) {
	run := &recordingRunner{}
	h := &Host{Run: run}
	for _, unit := range []string{"ssh.service", "sshd.service", "docker.service", "containerd.service"} {
		err := h.ServiceAction(context.Background(), unit, "stop")
		if _, ok := err.(*RefusedError); !ok {
			t.Errorf("stop %s: want RefusedError, got %v", unit, err)
		}
	}
	if len(run.cmds) != 0 {
		t.Fatalf("refused actions must not run anything, ran %v", run.cmds)
	}
	if err := h.ServiceAction(context.Background(), "nginx.service", "restart"); err != nil {
		t.Fatal(err)
	}
	if len(run.cmds) != 1 || run.cmds[0] != "systemctl restart --no-block nginx.service" {
		t.Fatalf("unexpected commands %v", run.cmds)
	}
}

func TestServiceActionRejectsInjection(t *testing.T) {
	run := &recordingRunner{}
	h := &Host{Run: run}
	for _, unit := range []string{"x;reboot.service", "../etc.service", "a b.service", "nginx", "$(id).service", "--force.service"} {
		if err := h.ServiceAction(context.Background(), unit, "restart"); err == nil {
			t.Errorf("unit %q accepted", unit)
		}
	}
	if err := h.ServiceAction(context.Background(), "nginx.service", "reload;id"); err == nil {
		t.Error("bad action accepted")
	}
	if len(run.cmds) != 0 {
		t.Fatalf("ran %v", run.cmds)
	}
}

func TestProtectedContainersNeedConfirmation(t *testing.T) {
	docker, fd := startFakeDocker(t)
	h := &Host{Run: &recordingRunner{}, Docker: docker}
	ctx := context.Background()
	err := h.ContainerAction(ctx, "traefik", "restart", "", ContainerFilter{})
	if _, ok := err.(*RefusedError); !ok {
		t.Fatalf("want RefusedError for traefik without confirm, got %v", err)
	}
	if err := h.ContainerAction(ctx, "traefik", "restart", "wrong", ContainerFilter{}); err == nil {
		t.Fatal("wrong confirmation accepted")
	}
	for _, r := range fd.all() {
		if strings.Contains(r, "/restart") {
			t.Fatal("restarted a protected container without confirmation")
		}
	}
	if err := h.ContainerAction(ctx, "traefik", "restart", "traefik", ContainerFilter{}); err != nil {
		t.Fatal(err)
	}
	if err := h.ContainerAction(ctx, "app", "restart", "", ContainerFilter{}); err != nil {
		t.Fatal(err)
	}
	if err := h.ContainerAction(ctx, "app", "rm", "", ContainerFilter{}); err == nil {
		t.Fatal("unknown action accepted")
	}
}

// A user who links ~/.vscode-server (or its bin) somewhere else must not
// get root to delete files there.
func TestVSCodeRefusesSymlinkedPaths(t *testing.T) {
	victim := t.TempDir()
	for _, name := range []string{strings.Repeat("a", 40), strings.Repeat("c", 40)} {
		if err := os.MkdirAll(filepath.Join(victim, "bin", name), 0o755); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-72 * time.Hour)
		if name[0] == 'a' {
			mt = time.Now()
		}
		_ = os.Chtimes(filepath.Join(victim, "bin", name), mt, mt)
	}
	h := &Host{Run: &recordingRunner{}}

	for _, link := range []string{".vscode-server", ".vscode-server/bin"} {
		root := t.TempDir()
		h.Root = root
		home := filepath.Join(root, "home/u")
		if err := os.MkdirAll(home, 0o755); err != nil {
			t.Fatal(err)
		}
		target := victim
		if link == ".vscode-server/bin" {
			if err := os.MkdirAll(filepath.Join(home, ".vscode-server"), 0o755); err != nil {
				t.Fatal(err)
			}
			target = filepath.Join(victim, "bin")
		}
		if err := os.Symlink(target, filepath.Join(home, link)); err != nil {
			t.Fatal(err)
		}
		if got := h.staleVSCodeCopies("/home/u", ".vscode-server/bin", nil); len(got) != 0 {
			t.Errorf("link at %s: listed %+v", link, got)
		}
	}

	// A path that was fine when listed but is a link at removal time.
	root := t.TempDir()
	h.Root = root
	base := filepath.Join(root, "home/u/.vscode-server/bin")
	old := filepath.Join(base, strings.Repeat("c", 40))
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, strings.Repeat("a", 40)), 0o755); err != nil {
		t.Fatal(err)
	}
	mt := time.Now().Add(-72 * time.Hour)
	_ = os.Chtimes(old, mt, mt)
	got := h.staleVSCodeCopies("/home/u", ".vscode-server/bin", nil)
	if len(got) != 1 {
		t.Fatalf("want one stale copy, got %+v", got)
	}
	if err := os.RemoveAll(base); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(victim, "bin"), base); err != nil {
		t.Fatal(err)
	}
	if err := h.removeVSCodeCopy(got[0]); err == nil {
		t.Fatal("removed through a swapped-in symlink")
	}
	if _, err := os.Stat(filepath.Join(victim, "bin", strings.Repeat("c", 40))); err != nil {
		t.Fatal("victim directory was removed")
	}
}

func TestServiceActionRefusesPowerAndNetworkUnits(t *testing.T) {
	run := &recordingRunner{out: map[string]string{
		"systemctl list-units --all --plain --no-legend --no-pager -- wg-quick@wg0.service": "wg-quick@wg0.service loaded active exited WireGuard via wg-quick(8) for wg0",
	}}
	h := &Host{Run: run}
	ctx := context.Background()
	refused := []struct{ unit, action string }{
		{"systemd-poweroff.service", "start"},
		{"systemd-reboot.service", "restart"},
		{"systemd-kexec.service", "start"},
		{"systemd-soft-reboot.service", "start"},
		{"rescue.service", "start"},
		{"emergency.service", "restart"},
		{"debug-shell.service", "start"},
		{"systemd-fsck@dev-sda1.service", "start"},
		{"systemd-fsck@dev-sda1.service", "restart"},
		{"ufw.service", "stop"},
		{"ufw.service", "restart"},
		{"nftables.service", "start"},
		{"firewalld.service", "stop"},
		{"networking.service", "restart"},
		{"systemd-networkd.service", "stop"},
		{"NetworkManager.service", "restart"},
		{"netfilter-persistent.service", "stop"},
		{"iptables.service", "stop"},
	}
	for _, c := range refused {
		err := h.ServiceAction(ctx, c.unit, c.action)
		if _, ok := err.(*RefusedError); !ok {
			t.Errorf("%s %s: want RefusedError, got %v", c.action, c.unit, err)
		}
	}
	for _, cmd := range run.cmds {
		if !strings.HasPrefix(cmd, "systemctl list-units") {
			t.Fatalf("refused actions must not change anything, ran %q", cmd)
		}
	}
	run.cmds = nil
	if err := h.ServiceAction(ctx, "wg-quick@wg0.service", "restart"); err != nil {
		t.Fatalf("restart of a loaded instance: %v", err)
	}
	if err := h.ServiceAction(ctx, "ssh.service", "restart"); err != nil {
		t.Fatalf("restart ssh: %v", err)
	}
	if !IsProtectedUnit("ufw.service") || !IsProtectedUnit("ssh.service") {
		t.Error("network and ssh units must be listed as protected")
	}
}

func TestAuthorizedKeysSkipsFIFOAndLinks(t *testing.T) {
	root := t.TempDir()
	h := &Host{Root: root}
	dir := filepath.Join(root, "home/u/.ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "authorized_keys"), 0o600); err != nil {
		t.Skip("mkfifo:", err)
	}
	done := make(chan struct{})
	go func() {
		h.authorizedKeys("/home/u/.ssh/authorized_keys")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO blocked")
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(dir, "authorized_keys2")); err != nil {
		t.Fatal(err)
	}
	if got, gaps := h.authorizedKeys("/home/u/.ssh/authorized_keys2"); got != nil || len(gaps) != 1 {
		t.Fatalf("followed a symlink: %+v %v", got, gaps)
	}
}
