package hostops

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

func TestAPTUpgradeReportsPendingServiceRestarts(t *testing.T) {
	root := t.TempDir()
	tool := filepath.Join(root, "usr/sbin/needrestart")
	if err := os.MkdirAll(filepath.Dir(tool), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool, nil, 0755); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{out: map[string]string{
		"apt-get -s":                            "Inst tmux [1.0] (1.1 Ubuntu:24.04/noble-updates [arm64])",
		"apt-get install":                       "Setting up tmux (1.1) ...\npackages have been installed but needrestart is suspended\n",
		"dpkg-query":                            "tmux\t1.1",
		"/usr/sbin/needrestart -b -m a -r l -l": "NEEDRESTART-VER: 3.6\nNEEDRESTART-SVC: ssh.service\nNEEDRESTART-SESS: private @ session #1\n",
	}}
	h := &Host{Root: root, Run: runner}
	ctx := context.Background()
	if err := h.UpgradeExec(ctx, UpgradeOptions{RunID: 5, Mode: proto.UpdateAll}); err != nil {
		t.Fatal(err)
	}
	got, err := h.UpgradeStatus(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	log := strings.Join(got.LogTail, "\n")
	if got.State != "succeeded" || !slices.Equal(got.Packages, []string{"tmux"}) ||
		!strings.Contains(log, "Systemd services using outdated libraries:\nssh.service") ||
		strings.Contains(log, "private @ session") {
		t.Fatalf("unexpected package or restart report: %+v", got)
	}
	last := runner.cmds[len(runner.cmds)-1]
	if last != "/usr/sbin/needrestart -b -m a -r l -l" {
		t.Fatalf("expected a list-only scan after the package transaction, got %q", last)
	}
}
