package hostops

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// skipFSTypes are pseudo or virtual filesystems left out of disk use.
var skipFSTypes = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "overlay": true, "squashfs": true, "proc": true,
	"sysfs": true, "cgroup": true, "cgroup2": true, "devpts": true, "mqueue": true,
	"debugfs": true, "tracefs": true, "securityfs": true, "pstore": true, "bpf": true,
	"configfs": true, "fusectl": true, "hugetlbfs": true, "autofs": true, "efivarfs": true,
	"binfmt_misc": true, "nsfs": true, "ramfs": true, "rpc_pipefs": true, "fuse.lxcfs": true,
	"nfsd": true, "selinuxfs": true, "zfs": false,
}

// Status collects the snapshot the poller stores every minute.
func (h *Host) Status(ctx context.Context) (proto.Status, error) {
	s := proto.Status{CollectedAt: time.Now().UTC(), Arch: runtime.GOARCH, CPUCount: runtime.NumCPU()}
	s.Hostname, _ = os.Hostname()
	s.OS = h.osRelease()
	if b, err := h.readFile("/proc/sys/kernel/osrelease"); err == nil {
		s.Kernel = strings.TrimSpace(string(b))
	}

	// CPU: two samples one second apart.
	a, errA := h.cpuTimes()
	select {
	case <-ctx.Done():
		return s, ctx.Err()
	case <-time.After(time.Second):
	}
	b, errB := h.cpuTimes()
	if errA == nil && errB == nil && b.total > a.total {
		busy := float64((b.total - b.idle) - (a.total - a.idle))
		s.CPUPercent = round1(100 * busy / float64(b.total-a.total))
	}

	if raw, err := h.readFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(raw))
		if len(f) >= 3 {
			s.Load1, _ = strconv.ParseFloat(f[0], 64)
			s.Load5, _ = strconv.ParseFloat(f[1], 64)
			s.Load15, _ = strconv.ParseFloat(f[2], 64)
		}
	}
	if raw, err := h.readFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(raw)); len(f) > 0 {
			up, _ := strconv.ParseFloat(f[0], 64)
			s.UptimeSeconds = int64(up)
			s.BootTime = s.CollectedAt.Add(-time.Duration(up * float64(time.Second))).Truncate(time.Second)
		}
	}
	h.memory(&s)
	s.Disks = h.disks()
	s.Net = h.netDev()

	if out, err := h.output(ctx, 5*time.Second, "timedatectl", "show", "-p", "NTPSynchronized", "--value"); err == nil {
		s.TimeSynced = out == "yes"
	}
	if _, err := os.Stat(h.path("/var/run/reboot-required")); err == nil {
		s.RebootRequired = true
		if raw, err := h.readFile("/var/run/reboot-required.pkgs"); err == nil {
			s.RebootPackages = uniqueLines(string(raw))
		}
	}
	s.FailedUnits = h.failedUnits(ctx)

	if h.Docker != nil {
		if d, err := h.dockerSummary(ctx); err == nil {
			s.Docker = &d
		}
	}
	return s, nil
}

func (h *Host) osRelease() string {
	raw, err := h.readFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return runtime.GOOS
}

type cpuSample struct{ total, idle uint64 }

func (h *Host) cpuTimes() (cpuSample, error) {
	raw, err := h.readFile("/proc/stat")
	if err != nil {
		return cpuSample{}, err
	}
	line, _, _ := strings.Cut(string(raw), "\n")
	f := strings.Fields(line)
	var s cpuSample
	for i, v := range f[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		if i >= 8 { // guest and guest_nice are already counted in user and nice
			break
		}
		s.total += n
		if i == 3 || i == 4 { // idle, iowait
			s.idle += n
		}
	}
	return s, nil
}

func (h *Host) memory(s *proto.Status) {
	raw, err := h.readFile("/proc/meminfo")
	if err != nil {
		return
	}
	vals := map[string]uint64{}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(f[0], 10, 64)
		vals[k] = n * 1024
	}
	s.MemTotal = vals["MemTotal"]
	s.MemAvailable = vals["MemAvailable"]
	if s.MemTotal > s.MemAvailable {
		s.MemUsed = s.MemTotal - s.MemAvailable
	}
	s.SwapTotal = vals["SwapTotal"]
	if s.SwapTotal > vals["SwapFree"] {
		s.SwapUsed = s.SwapTotal - vals["SwapFree"]
	}
}

func (h *Host) disks() []proto.Disk {
	raw, err := h.readFile("/proc/self/mounts")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []proto.Disk
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || skipFSTypes[f[2]] || !strings.HasPrefix(f[0], "/") || seen[f[0]] {
			continue
		}
		mount := unescapeMount(f[1])
		if strings.HasPrefix(mount, "/snap/") || strings.HasPrefix(mount, "/var/lib/docker/") {
			continue
		}
		total, used, avail, inodes, inodesUsed, err := statfs(mount)
		if err != nil || total == 0 {
			continue
		}
		seen[f[0]] = true
		out = append(out, proto.Disk{
			Mount: mount, Device: f[0], FSType: f[2],
			Total: total, Used: used, Avail: avail, InodesTotal: inodes, InodesUsed: inodesUsed,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mount < out[j].Mount })
	return out
}

// unescapeMount decodes the octal escapes /proc/mounts uses for spaces and tabs.
func unescapeMount(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

func (h *Host) netDev() []proto.NetIf {
	raw, err := h.readFile("/proc/net/dev")
	if err != nil {
		return nil
	}
	var out []proto.NetIf
	for _, line := range strings.Split(string(raw), "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "lo" || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "br-") || name == "docker0" {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		out = append(out, proto.NetIf{Name: name, RxBytes: rx, TxBytes: tx})
	}
	return out
}

func (h *Host) failedUnits(ctx context.Context) []string {
	out, err := h.output(ctx, 10*time.Second, "systemctl", "list-units", "--state=failed", "--output=json", "--no-pager")
	if err != nil {
		return []string{}
	}
	var units []struct {
		Unit string `json:"unit"`
	}
	if json.Unmarshal([]byte(out), &units) != nil {
		return []string{}
	}
	names := make([]string, 0, len(units))
	for _, u := range units {
		names = append(names, u.Unit)
	}
	return names
}

func (h *Host) dockerSummary(ctx context.Context) (proto.DockerSummary, error) {
	var d proto.DockerSummary
	v, err := h.Docker.version(ctx)
	if err != nil {
		return d, err
	}
	d.Version = v
	cs, err := h.Docker.containers(ctx)
	if err != nil {
		return d, err
	}
	d.Total = len(cs)
	for _, c := range cs {
		switch c.State {
		case "running":
			d.Running++
		case "restarting":
			d.Restarting++
		case "exited", "dead", "created":
			d.Exited++
		}
		if strings.Contains(c.Status, "(unhealthy)") {
			d.Unhealthy++
		}
	}
	if df, err := h.Docker.diskUsage(ctx); err == nil {
		d.ImagesBytes = df.LayersSize
		for _, c := range df.Containers {
			d.ContainersBytes += c.SizeRw
		}
		for _, v := range df.Volumes {
			if v.UsageData.Size > 0 {
				d.VolumesBytes += v.UsageData.Size
			}
		}
		for _, b := range df.BuildCache {
			if !b.Shared {
				d.BuildCacheBytes += b.Size
			}
		}
	}
	return d, nil
}

func uniqueLines(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" && !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
