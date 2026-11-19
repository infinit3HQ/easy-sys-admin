package hostops

import (
	"context"
	"os"
	"testing"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

func TestFinishedUpgradeIsNeverStartedOrExecutedAgain(t *testing.T) {
	runner := &recordingRunner{}
	h := &Host{Root: t.TempDir(), Run: runner}
	unlock, err := h.upgradeLock("prepare.lock")
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	old := proto.UpgradeResult{RunID: 123, State: "succeeded", Packages: []string{"openssl"}}
	if err := writeJSONAtomic(h.resultPath(123), old); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(h.resultPath(123))
	if err != nil {
		t.Fatal(err)
	}
	o := UpgradeOptions{RunID: 123, Mode: proto.UpdateSecurity}
	if _, err := h.UpgradeStart(context.Background(), "/fixed/helper", o); err != nil {
		t.Fatal(err)
	}
	if err := h.UpgradeExec(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(h.resultPath(123))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || len(runner.cmds) != 0 {
		t.Fatal("a finished run was executed or overwritten")
	}
}

func TestOnlyOneHelperCanExecuteAnUpgrade(t *testing.T) {
	h := &Host{Root: t.TempDir(), Run: &recordingRunner{}}
	unlock, err := h.upgradeLock("upgrade-exec.lock")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := h.UpgradeExec(context.Background(), UpgradeOptions{RunID: 8, Mode: proto.UpdateAll}); err == nil {
		t.Fatal("a second upgrade acquired execution")
	}
	if _, err := os.Stat(h.resultPath(8)); !os.IsNotExist(err) {
		t.Fatal("refusing a concurrent execution overwrote a result")
	}
}
