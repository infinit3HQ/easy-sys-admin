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
		h := &Host{Run: run, Docker: startComposeDocker(t, "app", labels)}
		_, err := h.ContainerUpdate(context.Background(), "app", "", ContainerFilter{})
		if _, ok := err.(*RefusedError); !ok {
			t.Errorf("%s: want RefusedError, got %v", name, err)
		}
		if len(run.cmds) != 0 {
			t.Errorf("%s: ran %v", name, run.cmds)
		}
	}

	run := &recordingRunner{}
	h := &Host{Run: run, Docker: startComposeDocker(t, "app", composeLabels("myapp", "web", dir, "compose.yml"))}
	if _, err := h.ContainerUpdate(context.Background(), "app", "", ContainerFilter{}); err != nil {
		t.Fatal(err)
	}
	want := "docker compose --project-directory " + dir + " -p myapp -f " + filepath.Join(dir, "compose.yml") + " pull web"
	if len(run.cmds) != 2 || run.cmds[0] != want {
		t.Fatalf("unexpected commands %v", run.cmds)
	}

	// A protected project still works with the container name confirmed.
	run = &recordingRunner{}
	h = &Host{Run: run, Docker: startComposeDocker(t, "app", composeLabels("traefik", "web", dir, "compose.yml"))}
	if _, err := h.ContainerUpdate(context.Background(), "app", "app", ContainerFilter{}); err != nil {
		t.Fatal(err)
	}
}
