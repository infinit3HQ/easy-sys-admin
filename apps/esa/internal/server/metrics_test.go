package server

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

var (
	sampleRe = regexp.MustCompile(`^([a-z_]+)(\{([^}]*)\})? (\S+)$`)
	labelRe  = regexp.MustCompile(`^([a-z_]+)="((?:[^"\\]|\\.)*)"$`)
)

func TestMetricsFormat(t *testing.T) {
	stats := fleet.Stats{
		ServersByStatus: map[fleet.ServerStatus]int64{fleet.StatusOK: 7, fleet.StatusUnreachable: 2},
		RunsRunning:     1, SSHConnections: 9, NotifyQueue: 3, NotifyDropped: 4,
		Poll: fleet.PollStats{
			LastCycleSeconds: 12.5, LastCycleServers: 9, LastCycleSkipped: 0,
			OK: 100, Failed: 6, Deferred: 8,
			DurationBuckets: []uint64{0, 50, 100, 101, 103, 106, 106}, DurationCount: 106, DurationSum: 140.25,
		},
	}
	var b bytes.Buffer
	writeMetrics(&b, stats, true, `v1 "x"`, "abc")
	out := b.String()

	allowedLabels := map[string]bool{"version": true, "commit": true, "status": true, "result": true, "le": true}
	typed := map[string]string{}
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if rest, ok := strings.CutPrefix(line, "# TYPE "); ok {
			f := strings.Fields(rest)
			typed[f[0]] = f[1]
			continue
		}
		if strings.HasPrefix(line, "# HELP ") {
			continue
		}
		m := sampleRe.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("not a valid sample line: %q", line)
		}
		name := m[1]
		base := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name, "_bucket"), "_sum"), "_count")
		if typed[name] == "" && typed[base] == "" {
			t.Errorf("%s has no # TYPE line before it", name)
		}
		if m[3] != "" {
			for _, l := range splitLabels(m[3]) {
				lm := labelRe.FindStringSubmatch(l)
				if lm == nil {
					t.Fatalf("bad label %q in %q", l, line)
				}
				if !allowedLabels[lm[1]] {
					t.Errorf("label %q is not in the fixed set: %q", lm[1], line)
				}
			}
		}
		if _, err := strconv.ParseFloat(m[4], 64); err != nil {
			t.Errorf("value of %q: %v", line, err)
		}
		values[name+m[2]] = m[4]
	}

	want := map[string]string{
		`esa_build_info{version="v1 \"x\"",commit="abc"}`: "1",
		`esa_db_up`:                                   "1",
		`esa_servers{status="ok"}`:                    "7",
		`esa_servers{status="unreachable"}`:           "2",
		`esa_servers{status="pending"}`:               "0",
		`esa_runs_running`:                            "1",
		`esa_poll_cycle_seconds`:                      "12.5",
		`esa_poll_cycle_servers`:                      "9",
		`esa_poll_cycle_skipped`:                      "0",
		`esa_polls_total{result="deferred"}`:          "8",
		`esa_poll_duration_seconds_bucket{le="1"}`:    "50",
		`esa_poll_duration_seconds_bucket{le="+Inf"}`: "106",
		`esa_poll_duration_seconds_sum`:               "140.25",
		`esa_ssh_connections`:                         "9",
		`esa_notify_queue_length`:                     "3",
		`esa_notify_dropped_total`:                    "4",
	}
	for k, v := range want {
		if values[k] != v {
			t.Errorf("%s = %q, want %q", k, values[k], v)
		}
	}
	for _, st := range fleet.StatusesForStats() {
		if _, ok := values[`esa_servers{status="`+string(st)+`"}`]; !ok {
			t.Errorf("status %s missing; every status must always be present", st)
		}
	}
	if typed["esa_polls_total"] != "counter" || typed["esa_poll_duration_seconds"] != "histogram" || typed["esa_servers"] != "gauge" {
		t.Errorf("types: %v", typed)
	}
}

func splitLabels(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote, esc := false, false
	for _, r := range s {
		switch {
		case esc:
			esc = false
		case r == '\\':
			esc = true
		case r == '"':
			inQuote = !inQuote
		case r == ',' && !inQuote:
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	return append(out, cur.String())
}
