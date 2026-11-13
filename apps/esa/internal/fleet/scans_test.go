package fleet

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const grypeFixture = `{
 "matches": [
  {"vulnerability": {"id": "USN-7000-1", "severity": "High", "fix": {"versions": ["2.39-0ubuntu8.10"], "state": "fixed"}},
   "relatedVulnerabilities": [{"id": "CVE-2026-1111"}],
   "artifact": {"name": "libc6", "version": "2.39-0ubuntu8.9", "type": "deb"}},
  {"vulnerability": {"id": "CVE-2026-1111", "severity": "High", "fix": {"versions": ["2.39-0ubuntu8.10"], "state": "fixed"}, "epss": [{"epss": 0.4}]},
   "relatedVulnerabilities": [{"id": "USN-7000-1"}],
   "artifact": {"name": "libc6", "version": "2.39-0ubuntu8.9", "type": "deb"}},
  {"vulnerability": {"id": "CVE-2026-2222", "severity": "Medium", "fix": {"state": "not-fixed"}, "knownExploited": [{"cve": "CVE-2026-2222"}]},
   "artifact": {"name": "openssl", "version": "3.0.13-0ubuntu3.5", "type": "deb"}},
  {"vulnerability": {"id": "CVE-2026-3333", "severity": "Low", "fix": {"state": "wont-fix"}},
   "artifact": {"name": "vim", "version": "2:9.1.0016-1ubuntu7", "type": "deb"}}
 ],
 "descriptor": {"name": "grype", "version": "0.87.0", "db": {"status": {"built": "2026-11-12T03:00:00Z"}}}
}`

func TestParseGrype(t *testing.T) {
	scanner, ver, built, ms, err := parseGrype([]byte(grypeFixture))
	if err != nil {
		t.Fatal(err)
	}
	if scanner != "grype" || ver != "0.87.0" || built == nil || built.Day() != 12 {
		t.Fatalf("descriptor: %s %s %v", scanner, ver, built)
	}
	if len(ms) != 3 {
		t.Fatalf("aliases of one advisory must be one match, got %d: %+v", len(ms), ms)
	}
	if ms[0].Package != "openssl" || ms[0].Tier != 1 {
		t.Fatalf("known exploited ranks first: %+v", ms[0])
	}
	if ms[1].Package != "libc6" || ms[1].Tier != 2 || ms[1].EPSS != 0.4 || len(ms[1].Aliases) != 1 {
		t.Fatalf("fixable high second, aliases merged: %+v", ms[1])
	}
	if ms[2].Reason != "the distribution will not fix it" || ms[2].Severity != "low" {
		t.Fatalf("upstream severity kept: %+v", ms[2])
	}
	if _, _, _, _, err := parseGrype([]byte(`{"matches":[],"descriptor":{"name":"trivy"}}`)); err == nil {
		t.Fatal("a report from another tool was read as Grype")
	}
	_, _, built, _, _ = parseGrype([]byte(`{"matches":[],"descriptor":{"name":"grype","db":{"built":"2026-11-01T00:00:00Z"}}}`))
	if built == nil {
		t.Fatal("the older db.built field was not read")
	}
}

func TestSBOMPurls(t *testing.T) {
	inv := proto.PackageInventory{Distro: "ubuntu", Release: "24.04", Packages: []proto.InstalledPackage{
		{Name: "libc6", Version: "2.39-0ubuntu8.9", Arch: "arm64", Source: "glibc", SourceVersion: "2.39-0ubuntu8.9"},
		{Name: "adduser", Version: "3.137ubuntu1", Arch: "all", Source: "adduser", SourceVersion: "3.137ubuntu1"},
		{Name: "vim", Version: "2:9.1.0016-1ubuntu7", Arch: "arm64", Source: "vim", SourceVersion: "2:9.1.0016-1ubuntu6"},
	}}
	bom := buildSBOM("web", inv, time.Now(), "dev")
	if bom.BOMFormat != "CycloneDX" || len(bom.Components) != 4 || bom.Components[0].Type != "operating-system" {
		t.Fatalf("bom: %+v", bom)
	}
	want := map[string]string{
		"libc6":   "pkg:deb/ubuntu/libc6@2.39-0ubuntu8.9?arch=arm64&distro=ubuntu-24.04&upstream=glibc",
		"adduser": "pkg:deb/ubuntu/adduser@3.137ubuntu1?arch=all&distro=ubuntu-24.04",
		"vim":     "pkg:deb/ubuntu/vim@2:9.1.0016-1ubuntu7?arch=arm64&distro=ubuntu-24.04&upstream=vim%402%3A9.1.0016-1ubuntu6",
	}
	for _, c := range bom.Components[1:] {
		if got := c.Purl; !strings.HasPrefix(got, "pkg:deb/ubuntu/") || got != strings.Replace(want[c.Name], "@2:", "@2:", 1) {
			t.Errorf("%s purl %s, want %s", c.Name, got, want[c.Name])
		}
	}
}

func TestIdleKernelStaysListedAfterTheRunningOne(t *testing.T) {
	if kernelRelease("linux-image-generic") != "" || kernelRelease("linux-libc-dev") != "" {
		t.Fatal("a meta package or linux-libc-dev was read as one kernel build")
	}
	if kernelRelease("linux-modules-7.0.0-1013-generic") != "7.0.0-1013-generic" {
		t.Fatal("old modules package")
	}
	if !sameKernel("7.0.0-1016", "7.0.0-1016-generic") || sameKernel("7.0.0-101", "7.0.0-1016-generic") {
		t.Fatal("headers without a flavor must match the running kernel, and a shorter ABI must not")
	}

	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "kernels")
	srv := addTestServer(t, s, a, nil, "mail")
	now := time.Now()
	snap := proto.Status{Kernel: "7.0.0-1016-generic", MemTotal: 100, MemUsed: 10, TimeSynced: true, FailedUnits: []string{},
		Disks: []proto.Disk{{Mount: "/", Total: 100, Used: 10}}}
	st := store.ServerState{ServerID: srv.ID, Status: "ok", Snapshot: store.MustJSON(snap), Reasons: store.JSON("[]"), LastSeenAt: &now}
	s.saveState(ctx, &st)

	raw := `{
	  "matches": [
	    {"vulnerability": {"id": "CVE-OLD", "severity": "Critical", "fix": {"versions": ["7.0.0-1016.17"], "state": "fixed"}, "knownExploited": [{}]},
	     "artifact": {"name": "linux-modules-7.0.0-1013-generic", "version": "7.0.0-1013.14", "type": "deb"}},
	    {"vulnerability": {"id": "CVE-LIB", "severity": "High", "fix": {"versions": ["2.39-0ubuntu8.10"], "state": "fixed"}},
	     "artifact": {"name": "libc6", "version": "2.39-0ubuntu8.9", "type": "deb"}},
	    {"vulnerability": {"id": "CVE-RUN", "severity": "High", "fix": {"versions": ["7.0.0-1017.1"], "state": "fixed"}},
	     "artifact": {"name": "linux-image-7.0.0-1016-generic", "version": "7.0.0-1016.17", "type": "deb"}},
	    {"vulnerability": {"id": "CVE-HDR", "severity": "Low", "fix": {"state": "not-fixed"}},
	     "artifact": {"name": "linux-headers-7.0.0-1016", "version": "7.0.0-1016.17", "type": "deb"}}
	  ],
	  "descriptor": {"name": "grype", "version": "0.121.3", "db": {"built": "` + s.now().UTC().Format(time.RFC3339) + `"}}
	}`
	v, err := s.ImportScan(ctx, a, srv.ID, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if v.RunningKernel != "7.0.0-1016-generic" || len(v.Matches) != 4 {
		t.Fatalf("scan: %+v", v)
	}
	if v.Matches[0].Package != "libc6" || v.Matches[1].Package != "linux-image-7.0.0-1016-generic" {
		t.Fatalf("running system should come first: %+v", v.Matches)
	}
	if v.Matches[1].NotRunningKernel || !v.Matches[3].NotRunningKernel || v.Matches[3].Package != "linux-modules-7.0.0-1013-generic" {
		t.Fatalf("only the old kernel is set aside: %+v", v.Matches)
	}
	if v.Matches[2].Package != "linux-headers-7.0.0-1016" || v.Matches[2].NotRunningKernel {
		t.Fatalf("headers for the running kernel were set aside: %+v", v.Matches[2])
	}
	if !strings.Contains(v.Coverage, "1 match is a kernel package") || !strings.Contains(v.Coverage, "7.0.0-1016-generic") {
		t.Fatalf("coverage: %s", v.Coverage)
	}
}

func TestLatestScanStates(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "scans")
	srv := store.Server{AccountID: a.AccountID, Name: "web", Host: "10.0.10.1", Port: 22, Username: "esa", Enrollment: "pending",
		ProtectedContainers: store.JSON("[]"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.db.Omit("Account", "Group").Create(&srv).Error; err != nil {
		t.Fatal(err)
	}
	none, err := s.LatestScan(ctx, a, srv.ID)
	if err != nil || none.Imported || !none.Stale || !strings.Contains(none.Coverage, "not a clean result") {
		t.Fatalf("no import: %+v %v", none, err)
	}
	v, err := s.ImportScan(ctx, a, srv.ID, []byte(strings.Replace(grypeFixture, "2026-11-12T03:00:00Z", s.now().Add(-time.Hour).UTC().Format(time.RFC3339), 1)))
	if err != nil || !v.Imported || v.Stale || len(v.Matches) != 3 {
		t.Fatalf("fresh import: %+v %v", v.Coverage, err)
	}
	old, err := s.ImportScan(ctx, a, srv.ID, []byte(strings.Replace(grypeFixture, "2026-11-12T03:00:00Z", "2026-10-01T00:00:00Z", 1)))
	if err != nil || !old.Stale || !strings.Contains(old.Coverage, "more than 2 days") {
		t.Fatalf("old database: %+v %v", old.Coverage, err)
	}
	viewer := a
	viewer.Role = RoleViewer
	if _, err := s.ImportScan(ctx, viewer, srv.ID, []byte(grypeFixture)); err == nil {
		t.Fatal("a viewer imported a scan")
	}
}
