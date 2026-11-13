package fleet

import (
	"context"
	"fmt"
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

// The playground server (mailserver, Ubuntu 24.04) was checked on 14 November
// 2026. It was running 7.0.0-1013-oracle while 7.0.0-1016 and two 6.17 builds
// were still installed. Meta packages must not be treated as a kernel build.
func TestPlaygroundKernelPackages(t *testing.T) {
	running := "7.0.0-1013-oracle"
	idle := []string{
		"linux-image-7.0.0-1016-oracle",
		"linux-modules-7.0.0-1016-oracle",
		"linux-headers-7.0.0-1016-oracle",
		"linux-tools-7.0.0-1016-oracle",
		"linux-image-6.17.0-1011-oracle",
		"linux-modules-extra-6.17.0-1011-oracle",
		"linux-image-6.17.0-1018-oracle",
		"linux-modules-6.17.0-1018-oracle",
	}
	runningPkgs := []string{
		"linux-image-7.0.0-1013-oracle",
		"linux-modules-7.0.0-1013-oracle",
		"linux-headers-7.0.0-1013-oracle",
		"linux-tools-7.0.0-1013-oracle",
	}
	meta := []string{"linux-image-oracle", "linux-headers-oracle", "linux-tools-common", "linux-libc-dev"}
	for _, name := range meta {
		if kernelRelease(name) != "" {
			t.Fatalf("%s was read as one kernel build", name)
		}
	}
	var matches []VulnMatch
	for _, name := range append(append([]string{}, runningPkgs...), idle...) {
		matches = append(matches, VulnMatch{ID: "CVE-K", Package: name, Severity: "High", FixState: "fixed"})
	}
	if n := markIdleKernels(matches, running); n != len(idle) {
		t.Fatalf("flagged %d, want %d", n, len(idle))
	}
	for _, m := range matches {
		wantIdle := kernelRelease(m.Package) != "" && !sameKernel(kernelRelease(m.Package), running)
		if m.NotRunningKernel != wantIdle {
			t.Fatalf("%s idle=%v", m.Package, m.NotRunningKernel)
		}
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

func imageGrype(digest, tag, built string) string {
	return fmt.Sprintf(`{"matches":[{"vulnerability":{"id":"CVE-IMG","severity":"High","fix":{"state":"fixed","versions":["2"]}},"artifact":{"name":"openssl","version":"1","type":"deb","metadata":{"imageID":"%s","userInput":"%s"}}}],"descriptor":{"name":"grype","version":"0.121.3","db":{"built":"%s"}}}`, digest, tag, built)
}

func TestTagChangeDoesNotClearAnImageMatch(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "images")
	srv := addTestServer(t, s, a, nil, "mail")
	if err := s.db.Create(&store.ServerState{ServerID: srv.ID, Status: "ok", Reasons: store.JSON("[]")}).Error; err != nil {
		t.Fatal(err)
	}
	built := s.now().UTC().Format(time.RFC3339)
	empty := `{"matches":[],"descriptor":{"name":"grype","version":"0.121.3","db":{"built":"` + built + `"}}}`
	if _, err := s.ImportScan(ctx, a, srv.ID, []byte(imageGrype("sha256:abc", "nginx:1", built))); err != nil {
		t.Fatal(err)
	}
	// The image is still on disk under a new tag. The digest is unchanged.
	if err := s.db.Model(&store.ServerState{}).Where("server_id = ?", srv.ID).Updates(map[string]any{
		"images_read": true, "image_digests": store.MustJSON([]string{"sha256:abc"}),
	}).Error; err != nil {
		t.Fatal(err)
	}
	v, err := s.ImportScan(ctx, a, srv.ID, []byte(empty))
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Matches) != 1 || v.Matches[0].ImageDigest != "sha256:abc" || !strings.Contains(v.Coverage, "tag change") {
		t.Fatalf("same digest was cleared: %+v %s", v.Matches, v.Coverage)
	}
	// No digest was recorded, so a tag-only match cannot be cleared.
	if _, err := s.ImportScan(ctx, a, srv.ID, []byte(imageGrype("", "nginx:1", built))); err != nil {
		t.Fatal(err)
	}
	v, err = s.ImportScan(ctx, a, srv.ID, []byte(empty))
	if err != nil || len(v.Matches) != 1 || v.Matches[0].ImageTag != "nginx:1" {
		t.Fatalf("tag-only match was cleared: %+v %v", v.Matches, err)
	}
	other := addTestServer(t, s, a, nil, "other")
	if err := s.db.Create(&store.ServerState{ServerID: other.ID, Status: "ok", Reasons: store.JSON("[]"), ImagesRead: true, ImageDigests: store.MustJSON([]string{})}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportScan(ctx, a, other.ID, []byte(imageGrype("sha256:gone", "nginx:1", built))); err != nil {
		t.Fatal(err)
	}
	v, err = s.ImportScan(ctx, a, other.ID, []byte(empty))
	if err != nil || len(v.Matches) != 0 {
		t.Fatalf("a digest missing from a read image list stayed: %+v %v", v.Matches, err)
	}
}
