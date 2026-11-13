package hostops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// Inventory lists installed deb packages. It does not contact a network.
// A failed dpkg-query is an error, not an empty list.
func (h *Host) Inventory(ctx context.Context) (proto.PackageInventory, error) {
	rep := proto.PackageInventory{Packages: []proto.InstalledPackage{}, Gaps: []string{}}
	id, release := h.distro()
	rep.Distro, rep.Release = id, release
	out, err := h.output(ctx, 30*time.Second, "dpkg-query", "-W", "-f", `${db:Status-Status}\t${Package}\t${Version}\t${Architecture}\t${source:Package}\t${source:Version}\n`)
	if err != nil {
		return rep, fmt.Errorf("dpkg-query failed: %w", err)
	}
	pkgs, truncated := parseDpkg(out)
	if len(pkgs) == 0 {
		return rep, errors.New("dpkg-query listed no installed packages")
	}
	rep.Packages = pkgs
	if truncated {
		rep.Gaps = append(rep.Gaps, "truncated")
	}
	return rep, nil
}

const maxPackages = 20000

// parseDpkg reads status, name, version, and arch lines. Only packages in
// the "installed" state count; removed packages that left config files
// ("config-files") and half-installed ones do not.
func parseDpkg(out string) (pkgs []proto.InstalledPackage, truncated bool) {
	pkgs = []proto.InstalledPackage{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if (len(f) != 4 && len(f) != 6) || f[0] != "installed" || !packageName(f[1]) {
			continue
		}
		if len(pkgs) == maxPackages {
			return pkgs, true
		}
		p := proto.InstalledPackage{Name: f[1], Version: f[2], Arch: f[3]}
		if len(f) == 6 && packageName(f[4]) {
			p.Source, p.SourceVersion = f[4], f[5]
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, false
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
