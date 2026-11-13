package hostops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecoveryFileDoesNotBecomeAGuarantee(t *testing.T) {
	dir := t.TempDir()
	ok := []byte(`{"schema":1,"tool":"restic","toolVersion":"0.16.4","dataset":"postgres","snapshot":"abc123","backup":{"state":"success","at":"` + time.Now().UTC().Format(time.RFC3339) + `"},"check":{"kind":"full","state":"success"},"restore":{"state":"not run"},"smoke":{"state":"not run"}}`)
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
	if rep.Sources[0].Freshness != "current" || !strings.Contains(rep.Sources[0].Note, "No successful restore") {
		t.Fatalf("a current backup is not a restore: %+v", rep.Sources[0])
	}
	if len(rep.Gaps) != 1 || rep.Gaps[0] != "borg.json: schema" {
		t.Fatalf("gaps: %+v", rep.Gaps)
	}
	if _, err := parseRecovery([]byte(`{"schema":1,"tool":"restic","dataset":"db","snapshot":"s3://secret"}`)); err == nil {
		t.Fatal("a repository URL must not be stored")
	}
	if _, err := parseRecovery([]byte(`{"schema":1,"tool":"restic"`)); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
	empty := readRecovery(t.TempDir())
	if len(empty.Sources) != 0 || len(empty.Gaps) != 1 || empty.Gaps[0] != "not configured" {
		t.Fatalf("empty: %+v", empty)
	}

	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		raw  string
		want string
		note string
	}{
		{"warning", `{"schema":1,"tool":"borg","dataset":"home","snapshot":"s1","backup":{"state":"warning","at":"2026-11-13T00:00:00Z"},"check":{"kind":"sampled","state":"success","at":"2026-11-13T01:00:00Z"}}`, "current", "sampled check"},
		{"failed", `{"schema":1,"tool":"restic","dataset":"db","snapshot":"s2","backup":{"state":"failed","at":"2026-11-13T00:00:00Z"}}`, "unknown", "No successful restore"},
		{"no snapshot", `{"schema":1,"tool":"generic","dataset":"mail","backup":{"state":"success","at":"2026-11-13T00:00:00Z"},"check":{"kind":"full","state":"warning"}}`, "current", "No snapshot"},
		{"stale", `{"schema":1,"tool":"restic","dataset":"db","snapshot":"old","backup":{"state":"success","at":"2026-10-01T00:00:00Z"},"restore":{"state":"success"},"smoke":{"state":"failed"}}`, "stale", "smoke test failed"},
		{"future", `{"schema":1,"tool":"restic","dataset":"db","snapshot":"s3","backup":{"state":"success","at":"2027-01-01T00:00:00Z"}}`, "future", "in the future"},
		{"no time", `{"schema":1,"tool":"restic","dataset":"db","snapshot":"s4","backup":{"state":"success"}}`, "unknown", "no time"},
	}
	for _, c := range cases {
		src, err := parseRecovery([]byte(c.raw))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		fresh, note := recoveryNote(src, now)
		if fresh != c.want || !strings.Contains(note, c.note) {
			t.Fatalf("%s: freshness %s note %s", c.name, fresh, note)
		}
	}
}
