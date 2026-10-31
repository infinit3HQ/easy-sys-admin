package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/version"
)

// metrics serves GET /metrics in the Prometheus text exposition format
// (version 0.0.4), written by hand. Every series has a fixed set of labels
// (version, commit, status, result, le), so the output never grows with the
// fleet and never carries server names, hosts, emails, or tokens.
func metrics(st *store.Store, fl *fleet.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		dbUp := st.Ping(ctx) == nil
		stats, err := fl.Stats(ctx)
		if err != nil {
			dbUp = false
		}
		var b bytes.Buffer
		writeMetrics(&b, stats, dbUp, version.Version, version.Commit)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b.Bytes())
	}
}

func writeMetrics(b *bytes.Buffer, s fleet.Stats, dbUp bool, ver, commit string) {
	head := func(name, typ, help string) {
		fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
	}
	num := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

	head("esa_build_info", "gauge", "Version and commit of the control plane. Always 1.")
	fmt.Fprintf(b, "esa_build_info{version=\"%s\",commit=\"%s\"} 1\n", labelValue(ver), labelValue(commit))

	head("esa_db_up", "gauge", "1 when the database answered this scrape, 0 when it did not.")
	fmt.Fprintf(b, "esa_db_up %d\n", boolInt(dbUp))

	head("esa_servers", "gauge", "Servers in all accounts by status. Zero while the database is down.")
	for _, st := range fleet.StatusesForStats() {
		fmt.Fprintf(b, "esa_servers{status=\"%s\"} %d\n", st, s.ServersByStatus[st])
	}

	head("esa_runs_running", "gauge", "Maintenance runs in progress in all accounts.")
	fmt.Fprintf(b, "esa_runs_running %d\n", s.RunsRunning)

	p := s.Poll
	head("esa_poll_cycle_seconds", "gauge", "Duration of the last finished poll cycle.")
	fmt.Fprintf(b, "esa_poll_cycle_seconds %s\n", num(p.LastCycleSeconds))
	head("esa_poll_cycle_servers", "gauge", "Ready servers at the start of the last poll cycle.")
	fmt.Fprintf(b, "esa_poll_cycle_servers %d\n", p.LastCycleServers)
	head("esa_poll_cycle_skipped", "gauge", "Servers the last poll cycle had no time to start.")
	fmt.Fprintf(b, "esa_poll_cycle_skipped %d\n", p.LastCycleSkipped)

	head("esa_polls_total", "counter", "Scheduled polls by result. deferred counts servers skipped by back-off.")
	fmt.Fprintf(b, "esa_polls_total{result=\"ok\"} %d\n", p.OK)
	fmt.Fprintf(b, "esa_polls_total{result=\"failed\"} %d\n", p.Failed)
	fmt.Fprintf(b, "esa_polls_total{result=\"deferred\"} %d\n", p.Deferred)

	head("esa_poll_duration_seconds", "histogram", "Duration of scheduled polls, including the status call and storing the result.")
	for i, le := range fleet.PollDurationBuckets {
		var n uint64
		if i < len(p.DurationBuckets) {
			n = p.DurationBuckets[i]
		}
		fmt.Fprintf(b, "esa_poll_duration_seconds_bucket{le=\"%s\"} %d\n", num(le), n)
	}
	fmt.Fprintf(b, "esa_poll_duration_seconds_bucket{le=\"+Inf\"} %d\n", p.DurationCount)
	fmt.Fprintf(b, "esa_poll_duration_seconds_sum %s\n", num(p.DurationSum))
	fmt.Fprintf(b, "esa_poll_duration_seconds_count %d\n", p.DurationCount)

	head("esa_ssh_connections", "gauge", "Open pooled SSH connections to managed servers.")
	fmt.Fprintf(b, "esa_ssh_connections %d\n", s.SSHConnections)

	head("esa_notify_queue_length", "gauge", "Alert notifications waiting for delivery.")
	fmt.Fprintf(b, "esa_notify_queue_length %d\n", s.NotifyQueue)
	head("esa_notify_dropped_total", "counter", "Alert notifications dropped because the queue was full.")
	fmt.Fprintf(b, "esa_notify_dropped_total %d\n", s.NotifyDropped)
}

// labelValue escapes a label value for the text format.
func labelValue(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
