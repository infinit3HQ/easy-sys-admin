package hostops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryFileDoesNotBecomeAGuarantee(t *testing.T) {
	dir := t.TempDir()
	ok := []byte(`{"schema":1,"tool":"restic","toolVersion":"0.16.4","dataset":"postgres","snapshot":"abc123","backup":{"state":"success","at":"2026-11-01T00:00:00Z"},"check":{"kind":"structural","state":"success"},"restore":{"state":"not run"},"smoke":{"state":"not run"},"repository":"s3:secret"}`)
	if err := os.WriteFile(filepath.Join(dir, "restic.json"), ok, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "borg.json"), []byte(`{"schema":2,"tool":"borg"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rep := readRecovery(dir)
	if len(rep.Sources) != 1 || rep.Sources[0].BackupState != "success" || rep.Sources[0].RestoreState != "not run" {
		t.Fatalf("source: %+v", rep.Sources)
	}
	if rep.Sources[0].CheckKind != "structural" {
		t.Fatalf("check: %+v", rep.Sources[0])
	}
	if len(rep.Gaps) != 1 || rep.Gaps[0] != "borg.json: schema" {
		t.Fatalf("gaps: %+v", rep.Gaps)
	}
	if _, err := parseRecovery([]byte(`{"schema":1,"tool":"restic","dataset":"db","snapshot":"s3://secret"}`)); err == nil {
		t.Fatal("a repository URL must not be stored")
	}
	empty := readRecovery(t.TempDir())
	if len(empty.Sources) != 0 || len(empty.Gaps) != 1 || empty.Gaps[0] != "not configured" {
		t.Fatalf("empty: %+v", empty)
	}
}
