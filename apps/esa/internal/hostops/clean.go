package hostops

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// CleanOptions selects cleanups. Measure-only when Apply is false.
type CleanOptions struct {
	Apply       bool
	Only        []string // cleanup ids; empty means all
	JournalSize string   // for example "200M"
}

var journalSizeRe = regexp.MustCompile(`^([1-9][0-9]{0,5})([KMG])$`)

// Clean measures, and when opts.Apply is set, runs the selected cleanups.
// Each cleanup is independent: one failing does not stop the others.
func (h *Host) Clean(ctx context.Context, opts CleanOptions) (proto.CleanReport, error) {
	want := map[string]bool{}
	for _, id := range opts.Only {
		switch id {
		case proto.CleanBuildCache, proto.CleanVSCode, proto.CleanJournal:
			want[id] = true
		default:
			return proto.CleanReport{}, &BadRequestError{Reason: "unknown cleanup " + strconv.Quote(id)}
		}
	}
	all := len(want) == 0
	if opts.JournalSize == "" {
		opts.JournalSize = "200M"
	}
	if (all || want[proto.CleanJournal]) && !validJournalSize(opts.JournalSize) {
		return proto.CleanReport{}, &BadRequestError{Reason: "journal size must look like 200M or 1G and be at least 50M"}
	}

	rep := proto.CleanReport{Applied: opts.Apply}
	if all || want[proto.CleanBuildCache] {
		rep.Items = append(rep.Items, h.cleanBuildCache(ctx, opts.Apply))
	}
	if all || want[proto.CleanVSCode] {
		rep.Items = append(rep.Items, h.cleanVSCode(ctx, opts.Apply))
	}
	if all || want[proto.CleanJournal] {
		rep.Items = append(rep.Items, h.cleanJournal(ctx, opts.Apply, opts.JournalSize))
	}
	return rep, nil
}

func validJournalSize(s string) bool {
	m := journalSizeRe.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	n, _ := strconv.Atoi(m[1])
	switch m[2] {
	case "K":
		return n >= 50*1024
	case "M":
		return n >= 50
	}
	return true
}

// ---- Build cache -----------------------------------------------------------

func (h *Host) cleanBuildCache(ctx context.Context, apply bool) proto.CleanResult {
	r := proto.CleanResult{ID: proto.CleanBuildCache, Status: "measured"}
	if h.Docker == nil {
		r.Status, r.Reason = "skipped", "Docker is not installed"
		return r
	}
	df, err := h.Docker.diskUsage(ctx)
	if err != nil {
		r.Status, r.Error = "failed", err.Error()
		return r
	}
	inUse := false
	for _, b := range df.BuildCache {
		if b.InUse {
			inUse = true
		}
		if !b.InUse && !b.Shared {
			r.ReclaimableNow += b.Size
		}
	}
	if !apply {
		return r
	}
	if inUse || h.buildRunning() {
		r.Status, r.Reason = "skipped", "a build is running"
		return r
	}
	freed, err := h.Docker.pruneBuildCache(ctx)
	if err != nil {
		r.Status, r.Error = "failed", err.Error()
		return r
	}
	r.Status, r.Freed = "cleaned", freed
	return r
}

// buildRunning looks for build commands among running processes.
func (h *Host) buildRunning() bool {
	for _, args := range h.processArgs() {
		if isBuildCommand(args) {
			return true
		}
	}
	return false
}

func isBuildCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	bin := filepath.Base(args[0])
	has := func(words ...string) bool {
		for _, w := range words {
			for _, a := range args[1:] {
				if a == w {
					return true
				}
			}
		}
		return false
	}
	switch bin {
	case "docker":
		return has("build", "bake") || (has("buildx", "compose", "builder") && has("build", "bake"))
	case "docker-buildx", "buildctl":
		return has("build", "bake")
	case "docker-compose":
		return has("build")
	}
	return false
}

// processArgs returns the argv of every process, keyed by pid.
func (h *Host) processArgs() map[int][]string {
	out := map[int][]string{}
	entries, err := os.ReadDir(h.path("/proc"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		raw, err := h.readFile("/proc/" + e.Name() + "/cmdline")
		if err != nil || len(raw) == 0 {
			continue
		}
		out[pid] = strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	}
	return out
}

// ---- VS Code server copies ----------------------------------------------------

// stagingMinAge protects downloads that are still in progress.
const stagingMinAge = time.Hour

type vscodeCopy struct {
	path    string // home/sub/name
	home    string
	sub     string
	name    string
	uid     uint32 // owner of home, and of every directory below it
	modTime time.Time
	staging bool
	size    int64
}

func (h *Host) cleanVSCode(ctx context.Context, apply bool) proto.CleanResult {
	r := proto.CleanResult{ID: proto.CleanVSCode, Status: "measured"}
	inUse := h.pathsInUse()
	var remove []vscodeCopy
	for _, home := range h.homes() {
		for _, sub := range []string{".vscode-server/bin", ".vscode-server/cli/servers"} {
			remove = append(remove, h.staleVSCodeCopies(home, sub, inUse)...)
		}
	}
	for _, c := range remove {
		r.ReclaimableNow += c.size
	}
	if !apply {
		for _, c := range remove {
			r.Removed = append(r.Removed, c.path)
		}
		return r
	}
	for _, c := range remove {
		if err := ctx.Err(); err != nil {
			r.Error = err.Error()
			break
		}
		if err := h.removeVSCodeCopy(c); err != nil {
			r.Error = err.Error()
			continue
		}
		r.Freed += c.size
		r.Removed = append(r.Removed, c.path)
	}
	r.Status = "cleaned"
	if r.Error != "" {
		r.Status = "failed"
	}
	return r
}

// staleVSCodeCopies lists copies in home/sub that may be removed: every
// finished install except the newest, plus staging folders older than an
// hour, minus anything a running process uses. Symlinks are never followed,
// and every directory from home down must belong to the home's owner, so a
// user cannot point root at someone else's files.
func (h *Host) staleVSCodeCopies(home, sub string, inUse []string) []vscodeCopy {
	_, uid, ok := h.userDir(home, sub)
	if !ok {
		return nil
	}
	base := filepath.Join(home, sub)
	entries, err := os.ReadDir(h.path(base))
	if err != nil {
		return nil
	}
	var installs, staging []vscodeCopy
	for _, e := range entries {
		if e.Type()&fs.ModeSymlink != 0 || !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if owner, ok := fileOwner(info); !ok || owner != uid {
			continue
		}
		c := vscodeCopy{path: filepath.Join(base, e.Name()), home: home, sub: sub, name: e.Name(), uid: uid, modTime: info.ModTime()}
		c.staging = strings.HasSuffix(e.Name(), ".staging") || strings.Contains(e.Name(), "-staging")
		if c.staging {
			staging = append(staging, c)
		} else if strings.HasPrefix(e.Name(), "Stable-") || strings.HasPrefix(e.Name(), "Insiders-") || isCommitHash(e.Name()) {
			installs = append(installs, c)
		}
	}
	sort.Slice(installs, func(i, j int) bool { return installs[i].modTime.After(installs[j].modTime) })

	var out []vscodeCopy
	keep := func(c vscodeCopy) bool {
		for _, p := range inUse {
			if p == c.path || strings.HasPrefix(p, c.path+"/") {
				return true
			}
		}
		return false
	}
	for i, c := range installs {
		if i == 0 || keep(c) {
			continue // newest copy, or one in use
		}
		c.size = h.dirSize(c.path)
		out = append(out, c)
	}
	for _, c := range staging {
		if time.Since(c.modTime) < stagingMinAge || keep(c) {
			continue
		}
		c.size = h.dirSize(c.path)
		out = append(out, c)
	}
	return out
}

// userDir checks home and every directory of sub below it with Lstat: each
// must be a real directory (not a symlink) owned by the home's owner. It
// returns the last directory and the owner.
func (h *Host) userDir(home, sub string) (fs.FileInfo, uint32, bool) {
	info, err := os.Lstat(h.path(home))
	if err != nil || !info.IsDir() {
		return nil, 0, false
	}
	uid, ok := fileOwner(info)
	if !ok {
		return nil, 0, false
	}
	p := home
	for _, part := range strings.Split(sub, "/") {
		p = filepath.Join(p, part)
		info, err = os.Lstat(h.path(p))
		if err != nil || !info.IsDir() {
			return nil, 0, false
		}
		if owner, ok := fileOwner(info); !ok || owner != uid {
			return nil, 0, false
		}
	}
	return info, uid, true
}

func fileOwner(info fs.FileInfo) (uint32, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Uid, true
}

// removeVSCodeCopy checks the path again right before removing, then
// removes the copy through a handle on the checked directory, so a path
// swapped in the meantime cannot redirect the removal.
func (h *Host) removeVSCodeCopy(c vscodeCopy) error {
	changed := fmt.Errorf("%s changed since it was checked; skipped", c.path)
	dir, uid, ok := h.userDir(c.home, c.sub)
	if !ok || uid != c.uid {
		return changed
	}
	root, err := os.OpenRoot(h.path(filepath.Join(c.home, c.sub)))
	if err != nil {
		return err
	}
	defer root.Close()
	if fi, err := root.Stat("."); err != nil || !os.SameFile(fi, dir) {
		return changed
	}
	fi, err := root.Lstat(c.name)
	if err != nil || !fi.IsDir() {
		return changed
	}
	if owner, ok := fileOwner(fi); !ok || owner != uid {
		return changed
	}
	return root.RemoveAll(c.name)
}

var commitHashRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

func isCommitHash(s string) bool { return commitHashRe.MatchString(s) }

// pathsInUse returns every process's executable and working directory.
func (h *Host) pathsInUse() []string {
	var out []string
	entries, err := os.ReadDir(h.path("/proc"))
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		for _, link := range []string{"exe", "cwd"} {
			if p, err := os.Readlink(h.path("/proc/" + e.Name() + "/" + link)); err == nil {
				out = append(out, strings.TrimSuffix(p, " (deleted)"))
			}
		}
	}
	return out
}

// homes returns /root and the home directory of every user in /etc/passwd
// whose home is under /home.
func (h *Host) homes() []string {
	out := []string{"/root"}
	raw, err := h.readFile("/etc/passwd")
	if err != nil {
		return out
	}
	seen := map[string]bool{"/root": true}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 7 {
			continue
		}
		home := filepath.Clean(f[5])
		if strings.HasPrefix(home, "/home/") && !seen[home] {
			seen[home] = true
			out = append(out, home)
		}
	}
	return out
}

func (h *Host) dirSize(p string) int64 {
	var n int64
	_ = filepath.WalkDir(h.path(p), func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// ---- Journal -------------------------------------------------------------------

var journalUsageRe = regexp.MustCompile(`take up ([0-9.]+)([KMGT]?)B?\b`)

func (h *Host) journalUsage(ctx context.Context) (int64, error) {
	out, err := h.output(ctx, 30*time.Second, "journalctl", "--disk-usage")
	if err != nil {
		return 0, err
	}
	m := journalUsageRe.FindStringSubmatch(out)
	if m == nil {
		return 0, fmt.Errorf("cannot parse journalctl --disk-usage: %q", out)
	}
	return parseSize(m[1], m[2]), nil
}

func parseSize(num, unit string) int64 {
	f, _ := strconv.ParseFloat(num, 64)
	mult := map[string]float64{"": 1, "K": 1 << 10, "M": 1 << 20, "G": 1 << 30, "T": 1 << 40}[unit]
	return int64(f * mult)
}

func (h *Host) cleanJournal(ctx context.Context, apply bool, size string) proto.CleanResult {
	r := proto.CleanResult{ID: proto.CleanJournal, Status: "measured"}
	before, err := h.journalUsage(ctx)
	if err != nil {
		r.Status, r.Error = "failed", err.Error()
		return r
	}
	m := journalSizeRe.FindStringSubmatch(size)
	limit := parseSize(m[1], m[2])
	if before > limit {
		r.ReclaimableNow = before - limit
	}
	if !apply {
		return r
	}
	if _, err := h.output(ctx, 5*time.Minute, "journalctl", "--vacuum-size="+size); err != nil {
		r.Status, r.Error = "failed", err.Error()
		return r
	}
	after, err := h.journalUsage(ctx)
	if err == nil && before > after {
		r.Freed = before - after
	}
	r.Status = "cleaned"
	return r
}
