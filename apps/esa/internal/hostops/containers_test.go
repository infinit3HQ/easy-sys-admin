package hostops

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// startComposeDocker serves one container whose labels come from labels,
// the way an image or compose would set them.
func startComposeDocker(t *testing.T, name string, labels map[string]string) *DockerClient {
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
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/containers/json":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"Id": "cccccccccccc3333", "Names": []string{"/" + name}, "State": "running", "Labels": labels},
			})
		case strings.HasSuffix(r.URL.Path, "/json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"Id": "cccccccccccc3333", "State": map[string]any{"Status": "running"}, "Config": map[string]any{"Labels": labels}})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return NewDockerClient(sock)
}

func composeLabels(project, service, dir, files string) map[string]string {
	return map[string]string{
		"com.docker.compose.project":              project,
		"com.docker.compose.service":              service,
		"com.docker.compose.project.working_dir":  dir,
		"com.docker.compose.project.config_files": files,
	}
}

func TestContainerUpdateChecksComposeLabels(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "evil.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "evil.yml"), filepath.Join(dir, "link.yml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.yml"), 0o755); err != nil {
		t.Fatal(err)
	}

	refused := map[string]map[string]string{
		"protected project":    composeLabels("traefik", "web", dir, "compose.yml"),
		"protected service":    composeLabels("edge", "traefik", dir, "compose.yml"),
		"option as service":    composeLabels("myapp", "-d", dir, "compose.yml"),
		"file outside dir":     composeLabels("myapp", "web", dir, filepath.Join(outside, "evil.yml")),
		"relative escape":      composeLabels("myapp", "web", dir, "../"+filepath.Base(outside)+"/evil.yml"),
		"symlinked file":       composeLabels("myapp", "web", dir, "link.yml"),
		"directory as file":    composeLabels("myapp", "web", dir, "sub.yml"),
		"missing file":         composeLabels("myapp", "web", dir, "gone.yml"),
		"no file label":        composeLabels("myapp", "web", dir, ""),
		"relative working dir": composeLabels("myapp", "web", "srv/app", "compose.yml"),
	}
	for name, labels := range refused {
		run := &recordingRunner{}
		h := &Host{Run: run, Docker: startComposeDocker(t, "app", labels), TrustedComposeOwner: anyOwner, TrustedComposeGroup: anyOwner}
		_, err := h.ContainerUpdate(context.Background(), "app", "", ContainerFilter{})
		if _, ok := err.(*RefusedError); !ok {
			t.Errorf("%s: want RefusedError, got %v", name, err)
		}
		if len(run.cmds) != 0 {
			t.Errorf("%s: ran %v", name, run.cmds)
		}
	}

	run := &recordingRunner{}
	h := &Host{Run: run, Docker: startComposeDocker(t, "app", composeLabels("myapp", "web", dir, "compose.yml")), TrustedComposeOwner: anyOwner, TrustedComposeGroup: anyOwner}
	if _, err := h.ContainerUpdate(context.Background(), "app", "", ContainerFilter{}); err != nil {
		t.Fatal(err)
	}
	want := "docker compose --project-directory " + dir + " -p myapp -f " + filepath.Join(dir, "compose.yml") + " pull web"
	if len(run.cmds) != 2 || run.cmds[0] != want {
		t.Fatalf("unexpected commands %v", run.cmds)
	}

	// A protected project still works with the container name confirmed.
	run = &recordingRunner{}
	h = &Host{Run: run, Docker: startComposeDocker(t, "app", composeLabels("traefik", "web", dir, "compose.yml")), TrustedComposeOwner: anyOwner, TrustedComposeGroup: anyOwner}
	if _, err := h.ContainerUpdate(context.Background(), "app", "app", ContainerFilter{}); err != nil {
		t.Fatal(err)
	}
}

func anyOwner(uint32) bool { return true }

// Compose runs as root on files named by labels that can come from an image.
// Files that someone who cannot act as root could change must be refused.
func TestContainerUpdateRefusesFilesOthersCanWrite(t *testing.T) {
	mk := func(dirMode, fileMode os.FileMode) string {
		dir := t.TempDir()
		if err := os.Chmod(dir, dirMode); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("services: {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(dir, "compose.yml"), fileMode); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	try := func(dir string, owner, group func(uint32) bool) (*recordingRunner, error) {
		run := &recordingRunner{}
		h := &Host{Run: run, Docker: startComposeDocker(t, "app", composeLabels("myapp", "web", dir, "compose.yml")),
			TrustedComposeOwner: owner, TrustedComposeGroup: group}
		_, err := h.ContainerUpdate(context.Background(), "app", "", ContainerFilter{})
		return run, err
	}
	no := func(uint32) bool { return false }
	for name, dir := range map[string]string{
		"world-writable file":      mk(0o755, 0o666),
		"world-writable directory": mk(0o777, 0o644),
		"group-writable file":      mk(0o755, 0o664),
		"group-writable directory": mk(0o775, 0o644),
	} {
		run, err := try(dir, anyOwner, no)
		if _, ok := err.(*RefusedError); !ok || len(run.cmds) != 0 {
			t.Errorf("%s: want RefusedError and no commands, got %v %v", name, err, run.cmds)
		}
	}
	// Group write is fine when the group is one that can act as root.
	if _, err := try(mk(0o775, 0o664), anyOwner, anyOwner); err != nil {
		t.Errorf("group-writable files of a trusted group: %v", err)
	}
	// World write is never fine.
	if run, err := try(mk(0o755, 0o666), anyOwner, anyOwner); err == nil || len(run.cmds) != 0 {
		t.Errorf("world-writable file of a trusted group: %v %v", err, run.cmds)
	}
	sound := mk(0o755, 0o644)
	if run, err := try(sound, no, anyOwner); err == nil || len(run.cmds) != 0 {
		t.Errorf("owner who cannot act as root: want refusal, got %v %v", err, run.cmds)
	}
	if _, err := try(sound, anyOwner, anyOwner); err != nil {
		t.Errorf("sound files with a trusted owner: %v", err)
	}
	// A .env beside the compose file is read by compose too.
	withEnv := mk(0o755, 0o644)
	if err := os.WriteFile(filepath.Join(withEnv, ".env"), []byte("IMAGE=x\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(withEnv, ".env"), 0o666); err != nil {
		t.Fatal(err)
	}
	if run, err := try(withEnv, anyOwner, anyOwner); err == nil || len(run.cmds) != 0 {
		t.Errorf("world-writable .env: want refusal, got %v %v", err, run.cmds)
	}
}

// A directory above the project that someone else owns lets them swap the
// project for another between the check and the run.
func TestContainerUpdateChecksParentDirectories(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "app")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	owner := func(uint32) bool { return true }
	if err := os.Chmod(parent, 0o777); err != nil {
		t.Fatal(err)
	}
	run := &recordingRunner{}
	h := &Host{Run: run, Docker: startComposeDocker(t, "app", composeLabels("myapp", "web", dir, "compose.yml")),
		TrustedComposeOwner: owner, TrustedComposeGroup: owner}
	if _, err := h.ContainerUpdate(context.Background(), "app", "", ContainerFilter{}); err == nil || len(run.cmds) != 0 {
		t.Fatalf("a world-writable parent directory was accepted: %v %v", err, run.cmds)
	}
	// With the sticky bit nobody can rename entries that are not theirs.
	if err := os.Chmod(parent, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	run = &recordingRunner{}
	h = &Host{Run: run, Docker: startComposeDocker(t, "app", composeLabels("myapp", "web", dir, "compose.yml")),
		TrustedComposeOwner: owner, TrustedComposeGroup: owner}
	if _, err := h.ContainerUpdate(context.Background(), "app", "", ContainerFilter{}); err != nil {
		t.Fatalf("a sticky world-writable parent: %v", err)
	}
}

// A symlink must not hide the directories that really hold the project.
func TestContainerUpdateResolvesSymlinkedProject(t *testing.T) {
	realParent := t.TempDir()
	if err := os.Chmod(realParent, 0o777); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(realParent, "app")
	if err := os.Mkdir(app, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "app")
	if err := os.Symlink(app, link); err != nil {
		t.Fatal(err)
	}
	run := &recordingRunner{}
	h := &Host{Run: run, Docker: startComposeDocker(t, "app", composeLabels("myapp", "web", link, "compose.yml")),
		TrustedComposeOwner: anyOwner, TrustedComposeGroup: anyOwner}
	if _, err := h.ContainerUpdate(context.Background(), "app", "", ContainerFilter{}); err == nil || len(run.cmds) != 0 {
		t.Fatalf("a project reached through a symlink under a world-writable directory was accepted: %v %v", err, run.cmds)
	}
}

func TestDockerGroupOwners(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(root, "etc", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("group", "root:x:0:\ndocker:x:998:ana\nstaff:x:50:bob\nsudo:x:27:dee\nana:x:1000:\nldap:x:5000:\n")
	write("passwd", "root:x:0:0::/root:/bin/sh\nana:x:1000:1000::/home/ana:/bin/sh\nbob:x:1001:50::/home/bob:/bin/sh\ncarl:x:1002:998::/home/carl:/bin/sh\ndee:x:1003:1003::/home/dee:/bin/sh\n")
	h := &Host{Root: root}
	trust := h.composeTrust()
	for uid, want := range map[uint32]bool{0: true, 1000: true, 1001: false, 1002: true, 1003: true, 4242: false} {
		if got := trust.uid(uid); got != want {
			t.Errorf("uid %d trusted = %v, want %v", uid, got, want)
		}
	}
	// A group nobody visible is in (5000) may have members only a directory
	// service knows, so it is not trusted.
	for gid, want := range map[uint32]bool{0: true, 998: true, 27: true, 50: false, 1000: true, 5000: false} {
		if got := trust.gid(gid); got != want {
			t.Errorf("gid %d trusted = %v, want %v", gid, got, want)
		}
	}
}
