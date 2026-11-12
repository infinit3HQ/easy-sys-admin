package server

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/auth"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

// startJobs launches the control plane's background loops.
//
//	every minute   poll servers, start due scheduled runs
//	every hour     roll up metrics, delete expired sessions, send the Monday summary
//	every 12 hours check TLS certificates
func startJobs(ctx context.Context, fl *fleet.Service, sessions *auth.Sessions, log *slog.Logger) {
	fl.ResumeRuns(ctx)
	go every(ctx, log, "poll", time.Minute, 5*time.Second, fl.PollAll)
	go every(ctx, log, "deliver", time.Minute, 10*time.Second, fl.DrainDeliveries)
	go every(ctx, log, "schedule", time.Minute, 15*time.Second, fl.Tick)
	go every(ctx, log, "rollup", time.Hour, 2*time.Minute, fl.Rollup)
	go every(ctx, log, "sessions", time.Hour, time.Minute, sessions.CleanupExpired)
	go every(ctx, log, "digest", time.Hour, 3*time.Minute, fl.SendDigests)
	go every(ctx, log, "certs", 12*time.Hour, 3*time.Minute, fl.CheckCerts)
	go every(ctx, log, "audit", time.Hour, 4*time.Minute, fl.PruneAudit)
}

// every runs fn after delay and then on each interval. A panic in one run
// is logged and does not stop the loop. Each run gets a deadline slightly
// under the interval, so a stuck run cannot hold up the next one.
func every(ctx context.Context, log *slog.Logger, name string, interval, delay time.Duration, fn func(context.Context)) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(delay):
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		func() {
			defer func() {
				if v := recover(); v != nil {
					log.Error("background job panicked", "job", name, "panic", v, "stack", string(debug.Stack()))
				}
			}()
			rctx, cancel := context.WithTimeout(ctx, interval-interval/12)
			defer cancel()
			fn(rctx)
		}()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
