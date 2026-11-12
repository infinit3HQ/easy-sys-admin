package hostops

import (
	"context"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// Inventory lists installed deb packages. It does not contact a network.
func (h *Host) Inventory(ctx context.Context) (proto.PackageInventory, error) {
	rep := proto.PackageInventory{Packages: []proto.InstalledPackage{}}
	id, release := h.distro()
	rep.Distro, rep.Release = id, release
	res, err := h.Run.Run(ctx, 30*time.Second, "dpkg-query", "-W", "-f", `${Package}\t${Version}\t${Architecture}\n`)
	if err != nil {
		rep.Gaps = []string{"dpkg"}
		return rep, nil
	}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		name, rest, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		version, arch, ok := strings.Cut(rest, "\t")
		if !ok || !packageName(name) {
			continue
		}
		rep.Packages = append(rep.Packages, proto.InstalledPackage{Name: name, Version: version, Arch: arch})
		if len(rep.Packages) == 20000 {
			rep.Gaps = append(rep.Gaps, "truncated")
			break
		}
	}
	return rep, nil
}

func (h *Host) distro() (id, release string) {
	raw, err := h.readFile("/etc/os-release")
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "ID="); ok {
			id = strings.Trim(v, `"`)
		}
		if v, ok := strings.CutPrefix(line, "VERSION_ID="); ok {
			release = strings.Trim(v, `"`)
		}
	}
	return id, release
}

func packageName(s string) bool {
	if s == "" || len(s) > 80 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '+' || r == '-' || r == '.':
		default:
			return false
		}
	}
	return true
}
