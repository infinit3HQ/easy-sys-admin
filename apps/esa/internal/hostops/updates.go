package hostops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// aptEnv makes apt and dpkg never wait for input and keep existing config
// files, and makes needrestart list services instead of restarting them.
var aptEnv = []string{
	"DEBIAN_FRONTEND=noninteractive",
	"NEEDRESTART_MODE=l",
	"NEEDRESTART_SUSPEND=1",
	"APT_LISTCHANGES_FRONTEND=none",
}

var aptDpkgOpts = []string{
	"-o", "Dpkg::Options::=--force-confdef",
	"-o", "Dpkg::Options::=--force-confold",
	"-o", "Dpkg::Lock::Timeout=120",
}

// "Inst base-files [13ubuntu10.4] (13ubuntu10.5 Ubuntu:24.04/noble-updates [arm64])"
var instRe = regexp.MustCompile(`^Inst (\S+) (?:\[([^\]]+)\] )?\((\S+) (.+?) \[[^\]]+\]\)`)

var pkgNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9+.\-]*(:[a-z0-9]+)?$`)

// PendingUpdates lists upgradable packages. With refresh it runs
// `apt-get update` first; otherwise it uses the lists apt already has.
func (h *Host) PendingUpdates(ctx context.Context, refresh bool) (proto.PendingUpdates, error) {
	if !lookPath("apt-get") {
		return proto.PendingUpdates{}, &BadRequestError{Reason: "this server does not use apt"}
	}
	if refresh {
		if busy := h.packageManagerBusy(); busy != "" {
			return proto.PendingUpdates{}, &BusyError{Reason: busy}
		}
		if _, err := h.output(ctx, 5*time.Minute, "apt-get", "update", "-q", "-o", "Dpkg::Lock::Timeout=60"); err != nil {
			return proto.PendingUpdates{}, err
		}
	}
	pkgs, err := h.simulateUpgrade(ctx)
	if err != nil {
		return proto.PendingUpdates{}, err
	}
	res := proto.PendingUpdates{Packages: pkgs, Held: h.heldPackages(ctx)}
	for _, p := range pkgs {
		if p.Security {
			res.Security++
		} else {
			res.Other++
		}
	}
	for _, stamp := range []string{"/var/lib/apt/periodic/update-success-stamp", "/var/lib/apt/lists"} {
		if fi, err := os.Stat(h.path(stamp)); err == nil {
			res.ListsUpdatedAt = fi.ModTime().UTC()
			break
		}
	}
	return res, nil
}

func (h *Host) simulateUpgrade(ctx context.Context) ([]proto.PendingPackage, error) {
	out, err := h.output(ctx, 2*time.Minute, "apt-get", "-s", "-q", "-o", "Debug::NoLocking=1", "upgrade")
	if err != nil {
		return nil, err
	}
	pkgs := parseSimulation(out)
	if len(pkgs) == 0 {
		return pkgs, nil
	}
	// apt-get -s names one archive per package, usually -updates, even when
	// the same version is also published in -security. Ask apt-cache which
	// sources offer each candidate version, as unattended-upgrades does.
	names := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		names = append(names, p.Name)
	}
	policy, err := h.output(ctx, 2*time.Minute, "apt-cache", append([]string{"policy"}, names...)...)
	if err == nil {
		sec := securityCandidates(policy)
		for i := range pkgs {
			pkgs[i].Security = pkgs[i].Security || sec[pkgs[i].Name]
		}
	}
	return pkgs, nil
}

// securityCandidates parses `apt-cache policy` and reports, per package,
// whether its candidate version is published in a -security pocket.
func securityCandidates(policy string) map[string]bool {
	out := map[string]bool{}
	var pkg, candidate string
	inCandidate := false
	for _, line := range strings.Split(policy, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case line != "" && !strings.HasPrefix(line, " ") && strings.HasSuffix(line, ":"):
			pkg, candidate, inCandidate = strings.TrimSuffix(line, ":"), "", false
		case strings.HasPrefix(trimmed, "Candidate:"):
			candidate = strings.TrimSpace(strings.TrimPrefix(trimmed, "Candidate:"))
		case candidate != "" && (strings.HasPrefix(trimmed, candidate+" ") || strings.HasPrefix(trimmed, "*** "+candidate+" ")):
			inCandidate = true
		case inCandidate && len(strings.Fields(trimmed)) >= 3 && isPriority(strings.Fields(trimmed)[0]):
			if strings.Contains(trimmed, "-security") {
				out[pkg] = true
			}
		case inCandidate:
			inCandidate = false
		}
	}
	return out
}

func isPriority(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func parseSimulation(out string) []proto.PendingPackage {
	pkgs := []proto.PendingPackage{}
	for _, line := range strings.Split(out, "\n") {
		m := instRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pkgs = append(pkgs, proto.PendingPackage{
			Name: m[1], From: m[2], To: m[3], Archive: m[4],
			Security: strings.Contains(m[4], "-security"),
		})
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].Name < pkgs[j].Name })
	return pkgs
}

func (h *Host) heldPackages(ctx context.Context) []string {
	out, err := h.output(ctx, 30*time.Second, "apt-mark", "showhold")
	if err != nil || out == "" {
		return []string{}
	}
	return strings.Fields(out)
}

// packageManagerBusy names a running package manager process, or returns "".
func (h *Host) packageManagerBusy() string {
	for _, args := range h.processArgs() {
		switch filepath.Base(args[0]) {
		case "apt", "apt-get", "dpkg", "aptitude", "unattended-upgrade", "unattended-upgrades":
			return filepath.Base(args[0]) + " is running"
		}
		// unattended-upgrade runs as a python script.
		for _, a := range args[1:] {
			if strings.HasSuffix(a, "/unattended-upgrade") {
				return "unattended-upgrade is running"
			}
		}
	}
	return ""
}

// minFree is the free space an upgrade needs on each filesystem.
var minFree = map[string]uint64{"/": 1 << 30, "/var": 1 << 30, "/boot": 150 << 20}

// UpgradePrecheck checks the server is in a state where an upgrade can run.
func (h *Host) UpgradePrecheck(ctx context.Context) (proto.Precheck, error) {
	pc := proto.Precheck{Problems: []string{}}
	if busy := h.packageManagerBusy(); busy != "" {
		pc.Problems = append(pc.Problems, busy)
	}
	if active := h.activeUpgradeUnit(ctx); active != "" {
		pc.Problems = append(pc.Problems, active+" is still running")
	}
	for mount, need := range minFree {
		_, _, avail, _, _, err := statfs(h.path(mount))
		if err != nil {
			continue
		}
		if avail < need {
			pc.Problems = append(pc.Problems, fmt.Sprintf("%s has %s free, needs %s", mount, humanBytes(avail), humanBytes(need)))
		}
	}
	if out, err := h.output(ctx, time.Minute, "dpkg", "--audit"); err != nil {
		pc.Problems = append(pc.Problems, "dpkg --audit failed: "+err.Error())
	} else if out != "" {
		pc.Problems = append(pc.Problems, "dpkg reports broken or half-installed packages: "+firstLine([]byte(out)))
	}
	pc.Held = h.heldPackages(ctx)
	sort.Strings(pc.Problems)
	pc.OK = len(pc.Problems) == 0
	return pc, nil
}

func (h *Host) activeUpgradeUnit(ctx context.Context) string {
	out, err := h.output(ctx, 10*time.Second, "systemctl", "list-units", "esa-upgrade-*", "--state=active,activating", "--output=json", "--no-pager")
	if err != nil {
		return ""
	}
	var units []struct {
		Unit string `json:"unit"`
	}
	if json.Unmarshal([]byte(out), &units) == nil && len(units) > 0 {
		return units[0].Unit
	}
	return ""
}

// UpgradeOptions configures one upgrade.
type UpgradeOptions struct {
	RunID int64
	Mode  proto.UpdateMode
	Hold  []string
	// Allow, when not nil, is the only packages this upgrade may install:
	// the list a person approved in a plan. An empty list installs nothing.
	Allow []string
}

func (o UpgradeOptions) validate() error {
	if o.RunID <= 0 {
		return &BadRequestError{Reason: "run id must be positive"}
	}
	if o.Mode != proto.UpdateSecurity && o.Mode != proto.UpdateAll {
		return &BadRequestError{Reason: "mode must be security or all"}
	}
	for _, p := range append(append([]string{}, o.Hold...), o.Allow...) {
		if !pkgNameRe.MatchString(p) {
			return &BadRequestError{Reason: "invalid package name " + strconv.Quote(p)}
		}
	}
	return nil
}

func upgradeUnit(runID int64) string { return fmt.Sprintf("esa-upgrade-%d", runID) }

func (h *Host) resultPath(runID int64) string {
	return h.path(filepath.Join(proto.StateDir, fmt.Sprintf("upgrade-%d.json", runID)))
}

// UpgradeStart launches the upgrade as a transient systemd unit, so it keeps
// running when the SSH session that started it drops.
func (h *Host) UpgradeStart(ctx context.Context, self string, o UpgradeOptions) (proto.UpgradeStarted, error) {
	if err := o.validate(); err != nil {
		return proto.UpgradeStarted{}, err
	}
	pc, err := h.UpgradePrecheck(ctx)
	if err != nil {
		return proto.UpgradeStarted{}, err
	}
	if !pc.OK {
		return proto.UpgradeStarted{}, &BusyError{Reason: strings.Join(pc.Problems, "; ")}
	}
	if err := os.MkdirAll(h.path(proto.StateDir), 0o750); err != nil {
		return proto.UpgradeStarted{}, err
	}
	initial := proto.UpgradeResult{RunID: o.RunID, State: "running", Mode: o.Mode, StartedAt: time.Now().UTC(), Packages: []string{}}
	if err := writeJSONAtomic(h.resultPath(o.RunID), initial); err != nil {
		return proto.UpgradeStarted{}, err
	}

	unit := upgradeUnit(o.RunID)
	args := []string{
		"--unit=" + unit,
		"--description=esa package upgrade (run " + strconv.FormatInt(o.RunID, 10) + ")",
		"--collect",
		"--property=KillMode=process", // never kill dpkg children mid-install
		"--property=TimeoutStopSec=1h",
	}
	for _, e := range aptEnv {
		args = append(args, "--setenv="+e)
	}
	args = append(args, self, "upgrade-exec", "--run-id", strconv.FormatInt(o.RunID, 10), "--mode", string(o.Mode))
	if len(o.Hold) > 0 {
		args = append(args, "--hold", strings.Join(o.Hold, ","))
	}
	if o.Allow != nil {
		allow := proto.AllowNone
		if len(o.Allow) > 0 {
			allow = strings.Join(o.Allow, ",")
		}
		args = append(args, "--allow", allow)
	}
	if _, err := h.output(ctx, 30*time.Second, "systemd-run", args...); err != nil {
		return proto.UpgradeStarted{}, err
	}
	return proto.UpgradeStarted{Unit: unit + ".service"}, nil
}

// UpgradeExec is the body of the transient unit. It writes its result file
// when done, whatever happens.
func (h *Host) UpgradeExec(ctx context.Context, o UpgradeOptions) error {
	if err := o.validate(); err != nil {
		return err
	}
	res := proto.UpgradeResult{RunID: o.RunID, State: "running", Mode: o.Mode, StartedAt: time.Now().UTC(), Packages: []string{}}
	if prev, err := readUpgradeResult(h.resultPath(o.RunID)); err == nil {
		res.StartedAt = prev.StartedAt
	}
	finish := func(err error, exit int, log []byte) error {
		res.FinishedAt = time.Now().UTC()
		res.ExitCode = exit
		res.LogTail = tailLines(string(log), 40)
		res.State = "succeeded"
		if err != nil {
			res.State, res.Error = "failed", err.Error()
		}
		_, statErr := os.Stat(h.path("/var/run/reboot-required"))
		res.RebootRequired = statErr == nil
		if werr := writeJSONAtomic(h.resultPath(o.RunID), res); werr != nil {
			return werr
		}
		return err
	}

	upd, err := h.Run.Run(ctx, 10*time.Minute, "apt-get", "update", "-q", "-o", "Dpkg::Lock::Timeout=120")
	if err != nil || upd.Exit != 0 {
		if err == nil {
			err = fmt.Errorf("apt-get update exited %d", upd.Exit)
		}
		return finish(err, upd.Exit, append(upd.Stdout, upd.Stderr...))
	}

	pending, err := h.simulateUpgrade(ctx)
	if err != nil {
		return finish(err, -1, nil)
	}
	install, skipped, notApproved := chooseUpgrades(pending, o)
	res.Skipped, res.NotApproved = skipped, notApproved
	if len(install) == 0 {
		return finish(nil, 0, []byte("nothing to upgrade"))
	}
	if o.Allow != nil {
		// apt can upgrade a dependency that was not asked for. With an
		// approved list that would widen consent, so stop instead.
		out, err := h.output(ctx, 2*time.Minute, "apt-get", append([]string{"-s", "-q", "-o", "Debug::NoLocking=1", "install", "--only-upgrade"}, install...)...)
		if err != nil {
			return finish(err, -1, nil)
		}
		if extra := unapproved(parseSimulation(out), o); len(extra) > 0 {
			return finish(fmt.Errorf("apt would also upgrade %s, which the plan did not approve; nothing was installed", strings.Join(extra, ", ")), -1, nil)
		}
	}

	args := append([]string{"install", "-y", "-q", "--only-upgrade"}, aptDpkgOpts...)
	args = append(args, install...)
	run, err := h.Run.Run(ctx, 2*time.Hour, "apt-get", args...)
	log := append(append(upd.Stdout, run.Stdout...), run.Stderr...)
	if err == nil && run.Exit != 0 {
		err = fmt.Errorf("apt-get install exited %d", run.Exit)
	}
	if err == nil {
		res.Packages = install
	}
	return finish(err, run.Exit, log)
}

// UpgradeStatus reads the result file. If the job died without writing a
// final result, it reports the run as failed.
func (h *Host) UpgradeStatus(ctx context.Context, runID int64) (proto.UpgradeResult, error) {
	res, err := readUpgradeResult(h.resultPath(runID))
	if errors.Is(err, os.ErrNotExist) {
		return proto.UpgradeResult{}, &BadRequestError{Reason: fmt.Sprintf("no upgrade with run id %d on this server", runID)}
	}
	if err != nil {
		return proto.UpgradeResult{}, err
	}
	if res.State == "running" {
		state, _ := h.output(ctx, 10*time.Second, "systemctl", "is-active", upgradeUnit(runID)+".service")
		if state != "active" && state != "activating" && state != "deactivating" {
			// Re-read: the job may have finished between the two reads.
			if again, err := readUpgradeResult(h.resultPath(runID)); err == nil && again.State != "running" {
				return again, nil
			}
			res.State, res.Error = "failed", "upgrade job ended without a result (unit state: "+state+")"
		}
	}
	return res, nil
}

func readUpgradeResult(path string) (proto.UpgradeResult, error) {
	var r proto.UpgradeResult
	raw, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(raw, &r)
}

func writeJSONAtomic(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func tailLines(s string, n int) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Reboot schedules a reboot five seconds out, so the SSH command that asked
// for it returns before the connection drops.
func (h *Host) Reboot(ctx context.Context) error {
	_, err := h.output(ctx, 30*time.Second, "systemd-run",
		"--unit=esa-reboot-"+strconv.FormatInt(time.Now().Unix(), 10),
		"--on-active=5", "--timer-property=AccuracySec=1s",
		"systemctl", "reboot")
	return err
}

// chooseUpgrades picks what to install from the pending list: not held,
// security only in security mode, and only approved packages when the
// run carries an approved list.
func chooseUpgrades(pending []proto.PendingPackage, o UpgradeOptions) (install, skipped, notApproved []string) {
	hold := map[string]bool{}
	for _, p := range o.Hold {
		hold[p] = true
	}
	allowed := map[string]bool{}
	for _, p := range o.Allow {
		allowed[p] = true
	}
	for _, p := range pending {
		switch {
		case hold[p.Name]:
			skipped = append(skipped, p.Name)
		case o.Mode == proto.UpdateSecurity && !p.Security:
		case o.Allow != nil && !allowed[p.Name]:
			notApproved = append(notApproved, p.Name)
		default:
			install = append(install, p.Name)
		}
	}
	return install, skipped, notApproved
}

// unapproved lists packages a simulated install would change that the
// approved list does not name, or that are held.
func unapproved(sim []proto.PendingPackage, o UpgradeOptions) []string {
	ok := map[string]bool{}
	for _, p := range o.Allow {
		ok[p] = true
	}
	for _, p := range o.Hold {
		ok[p] = false
	}
	var out []string
	for _, p := range sim {
		if !ok[p.Name] {
			out = append(out, p.Name)
		}
	}
	return out
}
