package hostops

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

const (
	recoveryDir     = "/var/lib/esa/recovery"
	recoveryMaxByte = 64 << 10
)

// Recovery reads operator-written restic and Borg result files. It does not
// run those tools, open a repository, or restore anything.
func (h *Host) Recovery(context.Context) (proto.RecoveryReport, error) {
	return readRecovery(recoveryDir), nil
}

func readRecovery(dir string) proto.RecoveryReport {
	rep := proto.RecoveryReport{
		Revision:  proto.ReportRevision,
		Statement: "These files were written on the server by something else. A success here is not proof the data can be restored.",
		Sources:   []proto.RecoverySource{},
	}
	for _, name := range []string{"restic.json", "borg.json"} {
		src, gap := readRecoveryFile(filepath.Join(dir, name))
		if gap == "missing" {
			continue
		}
		if gap != "" {
			rep.Gaps = append(rep.Gaps, name+": "+gap)
			continue
		}
		rep.Sources = append(rep.Sources, src)
	}
	if len(rep.Sources) == 0 && len(rep.Gaps) == 0 {
		rep.Gaps = []string{"not configured"}
	}
	return rep
}

// readRecoveryFile reads one result file. It does not follow a symlink,
// does not block on a FIFO, and reads only a regular file of at most
// recoveryMaxByte.
func readRecoveryFile(path string) (proto.RecoverySource, string) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return proto.RecoverySource{}, "missing"
		}
		return proto.RecoverySource{}, "unreadable"
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > recoveryMaxByte {
		return proto.RecoverySource{}, "unreadable"
	}
	buf, err := io.ReadAll(io.LimitReader(f, recoveryMaxByte+1))
	if err != nil || len(buf) > recoveryMaxByte {
		return proto.RecoverySource{}, "unreadable"
	}
	src, err := parseRecovery(buf)
	if err != nil {
		return proto.RecoverySource{}, err.Error()
	}
	return src, ""
}

func parseRecovery(buf []byte) (proto.RecoverySource, error) {
	var raw struct {
		Schema      int    `json:"schema"`
		Tool        string `json:"tool"`
		ToolVersion string `json:"toolVersion"`
		Dataset     string `json:"dataset"`
		Snapshot    string `json:"snapshot"`
		Backup      *struct {
			State string     `json:"state"`
			At    *time.Time `json:"at"`
		} `json:"backup"`
		Check *struct {
			Kind  string     `json:"kind"`
			State string     `json:"state"`
			At    *time.Time `json:"at"`
		} `json:"check"`
		Restore *struct {
			State string `json:"state"`
		} `json:"restore"`
		Smoke *struct {
			State string `json:"state"`
		} `json:"smoke"`
	}
	if err := json.Unmarshal(buf, &raw); err != nil {
		return proto.RecoverySource{}, errors.New("malformed")
	}
	if raw.Schema != 1 {
		return proto.RecoverySource{}, errors.New("schema")
	}
	tool := strings.ToLower(strings.TrimSpace(raw.Tool))
	if tool != "restic" && tool != "borg" && tool != "generic" {
		return proto.RecoverySource{}, errors.New("tool")
	}
	dataset, ok := cleanToken(raw.Dataset)
	if !ok {
		return proto.RecoverySource{}, errors.New("dataset")
	}
	snapshot, ok := cleanToken(raw.Snapshot)
	if raw.Snapshot != "" && !ok {
		return proto.RecoverySource{}, errors.New("snapshot")
	}
	src := proto.RecoverySource{
		Tool: tool, ToolVersion: trim(raw.ToolVersion, 40), Dataset: dataset, Snapshot: snapshot,
		BackupState: "unknown", CheckKind: "none", CheckState: "unknown",
		RestoreState: "not run", SmokeState: "not run",
	}
	if raw.Backup != nil {
		src.BackupState = oneOf(raw.Backup.State, "unknown", "success", "warning", "failed", "unknown")
		src.BackupAt = raw.Backup.At
	}
	if raw.Check != nil {
		src.CheckKind = oneOf(raw.Check.Kind, "none", "none", "structural", "sampled", "full")
		src.CheckState = oneOf(raw.Check.State, "unknown", "success", "warning", "failed", "unknown")
		src.CheckAt = raw.Check.At
	}
	if raw.Restore != nil {
		src.RestoreState = oneOf(raw.Restore.State, "not run", "not run", "success", "failed", "unknown")
	}
	if raw.Smoke != nil {
		src.SmokeState = oneOf(raw.Smoke.State, "not run", "not run", "success", "failed", "unknown")
	}
	return src, nil
}

func cleanToken(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 80 || strings.Contains(s, "://") || strings.Contains(s, "@") || strings.ContainsAny(s, " \t\n") {
		return "", false
	}
	return s, true
}

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

func oneOf(v, fallback string, allowed ...string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return fallback
}
