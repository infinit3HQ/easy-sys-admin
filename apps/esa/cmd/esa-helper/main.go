// Command esa-helper runs on managed servers. The control plane calls it over
// SSH as `sudo -n /usr/local/lib/esa/esa-helper <action> [flags]`. Every
// action prints one JSON envelope (see internal/hostops/proto) on stdout.
//
// The helper is deliberately small and has no network listener. It only
// does what the actions below describe.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/version"
)

const usage = `usage: esa-helper <action> [flags]

read actions:
  version | status | updates [--refresh] | upgrade-precheck | upgrade-status --run-id N
  services | containers [--protect a,b] | image-check | logs (--unit U | --container C) [--lines N] [--priority P] [--grep S]
  access | security | jobs | clean --measure [--only ids] [--journal-size 200M]

write actions:
  clean --apply [--only buildcache,vscode,journal] [--journal-size 200M]
  upgrade-start --run-id N --mode security|all [--hold pkg,pkg]
  service --unit U --action start|stop|restart
  container --name C --action start|stop|restart [--confirm NAME] [--protect a,b]
  container-update --name C [--confirm NAME] [--protect a,b]
  reboot
  uninstall   remove the esa user, its sudo rule, and this helper

ssh-dispatch is the esa key's forced command. It runs as the esa user and
passes "sudo -n esa-helper <action> ..." from SSH_ORIGINAL_COMMAND to sudo.
`

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if os.Args[1] == hostops.DispatchAction {
		os.Exit(sshDispatch())
	}
	// Programs are looked up on a fixed PATH, whatever the caller set.
	_ = os.Setenv("PATH", hostops.SafePath)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	data, err := run(ctx, os.Args[1], os.Args[2:])
	os.Exit(write(os.Stdout, data, err))
}

func write(w io.Writer, data any, err error) int {
	env := proto.Envelope{OK: err == nil, Protocol: proto.Version}
	if err == nil {
		raw, merr := json.Marshal(data)
		if merr != nil {
			err = merr
		} else {
			env.Data = raw
		}
	}
	if err != nil {
		env.OK, env.Error, env.Code = false, err.Error(), errorCode(err)
	}
	_ = json.NewEncoder(w).Encode(env)
	if err != nil {
		return 1
	}
	return 0
}

func errorCode(err error) string {
	var refused *hostops.RefusedError
	var bad *hostops.BadRequestError
	var busy *hostops.BusyError
	switch {
	case errors.As(err, &refused):
		return proto.CodeRefused
	case errors.As(err, &bad):
		return proto.CodeBadRequest
	case errors.As(err, &busy):
		return proto.CodeBusy
	}
	return proto.CodeFailed
}

func run(ctx context.Context, action string, args []string) (any, error) {
	fs := flag.NewFlagSet(action, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		refresh     = fs.Bool("refresh", false, "")
		measure     = fs.Bool("measure", false, "")
		apply       = fs.Bool("apply", false, "")
		only        = fs.String("only", "", "")
		journalSize = fs.String("journal-size", "200M", "")
		runID       = fs.Int64("run-id", 0, "")
		mode        = fs.String("mode", "", "")
		hold        = fs.String("hold", "", "")
		unit        = fs.String("unit", "", "")
		container   = fs.String("container", "", "")
		name        = fs.String("name", "", "")
		act         = fs.String("action", "", "")
		confirm     = fs.String("confirm", "", "")
		protect     = fs.String("protect", "", "")
		lines       = fs.Int("lines", 200, "")
		priority    = fs.Int("priority", -1, "")
		grep        = fs.String("grep", "", "")
	)
	if err := fs.Parse(args); err != nil {
		return nil, &hostops.BadRequestError{Reason: err.Error()}
	}
	if fs.NArg() > 0 {
		return nil, &hostops.BadRequestError{Reason: "unexpected argument " + fs.Arg(0)}
	}

	if action == "version" {
		return proto.VersionInfo{Version: version.Version, Commit: version.Commit, Protocol: proto.Version, OS: runtime.GOOS, Arch: runtime.GOARCH}, nil
	}
	if os.Geteuid() != 0 {
		return nil, &hostops.RefusedError{Reason: "esa-helper must run as root (through sudo)"}
	}

	h := hostops.NewHost()
	filter := hostops.ContainerFilter{}
	if *protect != "" {
		filter.Protected = split(*protect)
	}
	ctx, cancel := context.WithTimeout(ctx, timeoutFor(action))
	defer cancel()

	switch action {
	case "status":
		return h.Status(ctx)
	case "updates":
		return h.PendingUpdates(ctx, *refresh)
	case "upgrade-precheck":
		return h.UpgradePrecheck(ctx)
	case "upgrade-start":
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		return h.UpgradeStart(ctx, self, hostops.UpgradeOptions{RunID: *runID, Mode: proto.UpdateMode(*mode), Hold: split(*hold)})
	case "upgrade-exec":
		// Runs inside the transient systemd unit started by upgrade-start.
		err := h.UpgradeExec(ctx, hostops.UpgradeOptions{RunID: *runID, Mode: proto.UpdateMode(*mode), Hold: split(*hold)})
		return map[string]bool{"done": true}, err
	case "upgrade-status":
		return h.UpgradeStatus(ctx, *runID)
	case "clean":
		if *measure == *apply {
			return nil, &hostops.BadRequestError{Reason: "give exactly one of --measure or --apply"}
		}
		return h.Clean(ctx, hostops.CleanOptions{Apply: *apply, Only: split(*only), JournalSize: *journalSize})
	case "services":
		return h.Services(ctx)
	case "service":
		return map[string]string{"unit": *unit, "action": *act}, h.ServiceAction(ctx, *unit, *act)
	case "containers":
		return h.Containers(ctx, filter)
	case "container":
		return map[string]string{"container": *name, "action": *act}, h.ContainerAction(ctx, *name, *act, *confirm, filter)
	case "container-update":
		out, err := h.ContainerUpdate(ctx, *name, *confirm, filter)
		return map[string]string{"container": *name, "output": out}, err
	case "image-check":
		return h.ImageCheck(ctx, filter)
	case "logs":
		return h.Logs(ctx, hostops.LogOptions{Unit: *unit, Container: *container, Lines: *lines, Priority: *priority, Grep: *grep})
	case "access":
		return h.Access(ctx)
	case "security":
		return h.Security(ctx)
	case "jobs":
		return h.Jobs(ctx)
	case "reboot":
		return map[string]bool{"scheduled": true}, h.Reboot(ctx)
	case "uninstall":
		return map[string]bool{"scheduled": true}, h.Uninstall(ctx)
	}
	return nil, &hostops.BadRequestError{Reason: "unknown action " + action}
}

func timeoutFor(action string) time.Duration {
	switch action {
	case "upgrade-exec":
		return 3 * time.Hour
	case "clean", "updates", "container-update", "image-check":
		return 15 * time.Minute
	}
	return 2 * time.Minute
}

func split(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
