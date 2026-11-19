package hostops

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

func (h *Host) rocky9() bool {
	id, release := h.distro()
	return id == "rocky" && (release == "9" || strings.HasPrefix(release, "9."))
}

func normalizeRPMVersion(v string) string {
	return strings.TrimPrefix(strings.TrimPrefix(v, "0:"), "(none):")
}

func parseRPMInventory(out string) ([]proto.InstalledPackage, bool) {
	rows := []proto.InstalledPackage{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 5 || !pkgNameRe.MatchString(f[0]) || f[1] == "" || f[2] == "" || f[3] != "installed" {
			continue
		}
		if len(rows) == maxPackages {
			return rows, true
		}
		p := proto.InstalledPackage{Name: f[0], Version: normalizeRPMVersion(f[1]), Arch: f[2]}
		source := strings.TrimSuffix(f[4], ".src.rpm")
		parts := strings.Split(source, "-")
		if len(parts) >= 3 {
			p.Source = strings.Join(parts[:len(parts)-2], "-")
			p.SourceVersion = strings.Join(parts[len(parts)-2:], "-")
		}
		rows = append(rows, p)
	}
	return rows, false
}

func (h *Host) rpmInventory(ctx context.Context) ([]proto.InstalledPackage, bool, error) {
	out, err := h.output(ctx, time.Minute, "rpm", "-qa", "--qf", "%{NAME}\t%{EPOCHNUM}:%{VERSION}-%{RELEASE}\t%{ARCH}\tinstalled\t%{SOURCERPM}\n")
	if err != nil {
		return nil, false, errors.New("rpm could not read the installed package inventory")
	}
	pkgs, truncated := parseRPMInventory(out)
	if len(pkgs) == 0 {
		return nil, false, errors.New("rpm listed no installed packages")
	}
	return pkgs, truncated, nil
}

func (h *Host) dnfOutput(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	r, err := h.Run.Run(ctx, timeout, "dnf", args...)
	if err != nil || (r.Exit != 0 && r.Exit != 100) {
		return "", errors.New("DNF could not read complete package metadata; inspect the package-manager logs on this server")
	}
	return strings.TrimSpace(string(r.Stdout)), nil
}

func (h *Host) dnfSecurityMetadata() bool {
	paths, _ := filepath.Glob(h.path("/var/cache/dnf/*/repodata/repomd.xml"))
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var doc struct {
			Data []struct {
				Type string `xml:"type,attr"`
			} `xml:"data"`
		}
		if xml.Unmarshal(raw, &doc) == nil {
			for _, d := range doc.Data {
				if d.Type == "updateinfo" {
					return true
				}
			}
		}
	}
	return false
}

func (h *Host) dnfVersionLocks() bool {
	if raw, err := h.readFile("/etc/dnf/plugins/versionlock.conf"); err == nil {
		enabled := true
		for _, line := range strings.Split(string(raw), "\n") {
			if key, value, ok := strings.Cut(line, "="); ok && strings.EqualFold(strings.TrimSpace(key), "enabled") {
				enabled = strings.TrimSpace(value) != "0"
			}
		}
		if enabled {
			return true
		}
	} else if !os.IsNotExist(err) {
		return true
	}
	raw, err := h.readFile("/etc/dnf/plugins/versionlock.list")
	if err != nil {
		return !os.IsNotExist(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return true
		}
	}
	return false
}

func (h *Host) pendingRPM(ctx context.Context, refresh bool) (proto.PendingUpdates, error) {
	res := proto.PendingUpdates{Packages: []proto.PendingPackage{}, Held: []string{}}
	if h.dnfVersionLocks() {
		return res, &RefusedError{Reason: "system DNF versionlocks are configured; use distro tooling for this server or remove those locks explicitly"}
	}
	if refresh {
		if busy := h.packageManagerBusy(); busy != "" {
			return res, &BusyError{Reason: busy}
		}
		if _, err := h.dnfOutput(ctx, 5*time.Minute, "-q", "--setopt=*.skip_if_unavailable=False", "makecache", "--refresh"); err != nil {
			return res, err
		}
	}
	installed, truncated, err := h.rpmInventory(ctx)
	if err != nil {
		return res, err
	}
	if truncated {
		return res, errors.New("RPM inventory is truncated")
	}
	versions := map[string]string{}
	installedSpecs := map[string]bool{}
	for _, p := range installed {
		spec := p.Name + "." + p.Arch
		if installedSpecs[spec] && versions[spec] != p.Version {
			versions[spec] = ""
		} else if !installedSpecs[spec] {
			versions[spec] = p.Version
		}
		installedSpecs[spec] = true
	}
	out, err := h.dnfOutput(ctx, 5*time.Minute, "-q", "--setopt=*.skip_if_unavailable=False", "check-update")
	if err != nil {
		return res, err
	}
	if !h.dnfSecurityMetadata() {
		return res, errors.New("DNF security advisory metadata is missing; refresh package lists rather than treating this as no security updates")
	}
	security, err := h.dnfOutput(ctx, 2*time.Minute, "-q", "--cacheonly", "--setopt=*.skip_if_unavailable=False", "updateinfo", "list", "--updates", "--security")
	if err != nil {
		return res, err
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || !installedSpecs[f[0]] || !pkgNameRe.MatchString(f[0]) || seen[f[0]] {
			continue
		}
		last := strings.LastIndexByte(f[0], '.')
		if last < 0 {
			continue
		}
		base, architecture := f[0][:last], f[0][last+1:]
		sec := false
		for _, entry := range strings.Split(security, "\n") {
			fields := strings.Fields(entry)
			if len(fields) > 0 {
				nevra := fields[len(fields)-1]
				if strings.HasPrefix(nevra, base+"-") && strings.HasSuffix(nevra, "."+architecture) {
					sec = true
					break
				}
			}
		}
		res.Packages = append(res.Packages, proto.PendingPackage{Name: f[0], From: versions[f[0]], To: normalizeRPMVersion(f[1]), Archive: f[2], Security: sec})
		seen[f[0]] = true
		if sec {
			res.Security++
		} else {
			res.Other++
		}
	}
	sort.Slice(res.Packages, func(i, j int) bool { return res.Packages[i].Name < res.Packages[j].Name })
	if refresh {
		res.ListsUpdatedAt = time.Now().UTC()
	}
	return res, nil
}

// The resolved DNF transaction is inspected in the same process that commits
// it. A dependency, erasure, or architecture outside the approved set stops
// the transaction before download or installation. No shell is involved.
const dnfUpgradeScript = `# ESA_DNF_TXN
import json,sys,dnf,logging
logging.disable(logging.CRITICAL)
class Refused(Exception): pass
def safe_error(kind,value,traceback):
 print('ESA_DNF_ERROR '+(str(value) if isinstance(value,Refused) else 'transaction_failed'),file=sys.stderr)
sys.excepthook=safe_error
allowed=set(json.loads(sys.argv[1]))
with dnf.Base() as base:
 base.conf.read()
 base.conf.substitutions.update_from_etc(base.conf.installroot,base.conf.varsdir)
 base.conf.install_weak_deps=False
 base.conf.obsoletes=False
 base.conf.installonly_limit=0
 protected_kernels={'kernel','kernel-core','kernel-modules','kernel-modules-core','kernel-modules-extra'}
 base.conf.installonlypkgs=list(set(base.conf.installonlypkgs)|protected_kernels)
 base.conf.gpgcheck=True
 base.conf.localpkg_gpgcheck=True
 base.read_all_repos()
 for repo in base.repos.iter_enabled():
  repo.skip_if_unavailable=False
  repo.gpgcheck=True
 base.fill_sack()
 installed={p.name+'.'+p.arch for p in base.sack.query().installed()}
 if not allowed.issubset(installed):
  raise Refused('package_no_longer_installed')
 for spec in sorted(allowed):
  base.upgrade(spec)
 base.resolve(allow_erasing=False)
 installing=list(base.transaction.install_set)
 removing=list(base.transaction.remove_set)
 changed={p.name+'.'+p.arch for p in installing}
 if not changed.issubset(allowed):
  raise Refused('dependency_not_approved')
 if any(p.name+'.'+p.arch not in changed for p in removing):
  raise Refused('erasure_not_approved')
 if any(p.name in protected_kernels for p in removing):
  raise Refused('kernel_removal')
 base.download_packages(installing)
 for pkg in installing:
  status,_=base.package_signature_check(pkg)
  if status != 0:
   raise Refused('signing_key_untrusted')
 base.do_transaction()
 print('ESA_DNF_RESULT '+json.dumps(sorted(changed)))
`

func rpmUpgradeArgs(names []string) []string {
	// Names have already passed UpgradeOptions validation. JSON is an
	// argument to a fixed program, never a fragment of executable code.
	raw, _ := json.Marshal(names)
	return []string{"-c", dnfUpgradeScript, string(raw)}
}

func (h *Host) upgradeRPM(ctx context.Context, o UpgradeOptions, res *proto.UpgradeResult, finish func(error, int, []byte) error) error {
	pending, err := h.pendingRPM(ctx, true)
	if err != nil {
		return finish(err, -1, nil)
	}
	// A base name in the hold list also holds every installed architecture.
	held := append([]string{}, o.Hold...)
	for _, p := range pending.Packages {
		last := strings.LastIndexByte(p.Name, '.')
		if last >= 0 {
			for _, name := range o.Hold {
				if name == p.Name[:last] {
					held = append(held, p.Name)
				}
			}
		}
	}
	o.Hold = held
	install, skipped, notApproved := chooseUpgrades(pending.Packages, o)
	res.Skipped, res.NotApproved = skipped, notApproved
	if len(install) == 0 {
		return h.finishRPM(ctx, res, finish, []byte("nothing to upgrade"))
	}
	run, err := h.Run.Run(ctx, 2*time.Hour, "python3", rpmUpgradeArgs(install)...)
	if err != nil || run.Exit != 0 {
		reason := "DNF refused or failed the approved transaction; inspect the package-manager logs on this server"
		for code, message := range map[string]string{"dependency_not_approved": "DNF would install an unapproved dependency; nothing was installed", "erasure_not_approved": "DNF would erase a package without an approved replacement; nothing was installed", "kernel_removal": "DNF would erase an installed kernel; nothing was installed", "signing_key_untrusted": "a package signature or trusted signing key is missing; nothing was installed", "package_no_longer_installed": "an approved package is no longer installed; nothing was installed"} {
			if strings.Contains(string(run.Stderr), "ESA_DNF_ERROR "+code) {
				reason = message
				break
			}
		}
		return finish(errors.New(reason), run.Exit, nil)
	}
	var changed []string
	found := false
	for _, line := range strings.Split(string(run.Stdout), "\n") {
		if strings.HasPrefix(line, "ESA_DNF_RESULT ") {
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "ESA_DNF_RESULT ")), &changed) == nil {
				found = true
			}
		}
	}
	if !found {
		return finish(errors.New("DNF returned no complete transaction result"), -1, nil)
	}
	approved := map[string]bool{}
	for _, name := range install {
		approved[name] = true
	}
	for _, name := range changed {
		if !approved[name] {
			return finish(errors.New("DNF reported an unapproved package change"), -1, nil)
		}
	}
	res.Packages = changed
	pkgs, truncated, err := h.rpmInventory(ctx)
	if err != nil || truncated {
		return finish(errors.New("DNF finished but RPM could not confirm the installed versions"), -1, nil)
	}
	after := map[string]map[string]bool{}
	for _, p := range pkgs {
		spec := p.Name + "." + p.Arch
		if after[spec] == nil {
			after[spec] = map[string]bool{}
		}
		after[spec][p.Version] = true
	}
	for _, p := range pending.Packages {
		for _, name := range changed {
			if name == p.Name {
				if !after[name][p.To] {
					return finish(errors.New("DNF finished but the planned package version was not found"), -1, nil)
				}
				res.Versions = append(res.Versions, proto.PackageVersion{Name: name, Before: p.From, After: p.To})
			}
		}
	}
	for _, name := range install {
		present := false
		for _, actual := range changed {
			if name == actual {
				present = true
			}
		}
		if !present {
			res.Skipped = append(res.Skipped, name)
		}
	}
	return h.finishRPM(ctx, res, finish, []byte("approved DNF transaction completed"))
}

func (h *Host) finishRPM(ctx context.Context, res *proto.UpgradeResult, finish func(error, int, []byte) error, log []byte) error {
	check, err := h.Run.Run(ctx, time.Minute, "needs-restarting", "-r")
	if err != nil || (check.Exit != 0 && check.Exit != 1) {
		return finish(errors.New("RPM changes completed but the reboot requirement could not be checked; inspect this server before continuing"), -1, log)
	}
	res.RebootRequired = check.Exit == 1
	return finish(nil, 0, log)
}
