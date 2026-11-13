package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

func TestCompareDebVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"3.0.13-0ubuntu3.1", "3.0.13-0ubuntu3.2", -1},
		{"3.0.13-0ubuntu3.2", "3.0.13-0ubuntu3.2", 0},
		{"3.0.13-0ubuntu3.10", "3.0.13-0ubuntu3.2", 1},
		{"1.0~rc1", "1.0", -1},
		{"1:1.0", "2.0", 1},
		{"2.36.1-8+deb11u1", "2.36.1-8+deb11u2", -1},
		{"1.2a", "1.2+", -1},
		{"1.02", "1.2", 0},
	}
	for _, c := range cases {
		if got := compareDebVersions(c.a, c.b); got != c.want {
			t.Errorf("compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestMatchAdvisoriesComparesFixedVersion(t *testing.T) {
	path := writeFeed(t, `{"schema":1,"distro":"ubuntu","release":"24.04","advisories":[{"id":"USN-2","package":"openssl","fixed":"3.0.13-0ubuntu3.2","severity":"high","affected":[]}]}`)
	inv := proto.PackageInventory{Distro: "ubuntu", Release: "24.04", Packages: []proto.InstalledPackage{{Name: "openssl", Version: "3.0.13-0ubuntu3"}}}
	if got := matchAdvisories(inv, path); len(got.Matches) != 1 {
		t.Fatalf("an older version than fixed must match: %+v", got)
	}
	inv.Packages[0].Version = "3.0.13-0ubuntu3.2"
	if got := matchAdvisories(inv, path); len(got.Matches) != 0 {
		t.Fatalf("the fixed version must not match: %+v", got)
	}
}

func TestBackportAndSourcePackage(t *testing.T) {
	// 3.0.13-0ubuntu3.4 is the Ubuntu fix. 3.2.1 is the upstream release it
	// was taken from. 3.1.0 is newer than the Ubuntu package and older than
	// upstream, so comparing upstream would call the backport unfixed.
	path := writeFeed(t, `{"schema":1,"distro":"ubuntu","release":"24.04","advisories":[
	  {"id":"USN-9","package":"openssl","fixed":"3.0.13-0ubuntu3.4","upstreamFixed":"3.2.1","severity":"high"},
	  {"id":"USN-8","package":"glibc","fixed":"2.39-0ubuntu8.4","severity":"medium"},
	  {"id":"USN-7","package":"cron","fixed":"1:2.0","severity":"low"}
	]}`)
	inv := proto.PackageInventory{Distro: "ubuntu", Release: "24.04", Packages: []proto.InstalledPackage{
		{Name: "openssl", Version: "3.1.0"},
		{Name: "libc6", Version: "2.39-0ubuntu8.3", Source: "glibc", SourceVersion: "2.39-0ubuntu8.3"},
		{Name: "libc-bin", Version: "2.39-0ubuntu8.3", Source: "glibc", SourceVersion: "2.39-0ubuntu8.4"},
		{Name: "cron", Version: "2.1"},
	}}
	got := matchAdvisories(inv, path)
	if len(got.Matches) != 2 {
		t.Fatalf("matches: %+v", got.Matches)
	}
	if got.Matches[0].Package != "libc6" || got.Matches[0].Version != "2.39-0ubuntu8.3" {
		t.Fatalf("source advisory should hit the older binary: %+v", got.Matches[0])
	}
	if got.Matches[1].Package != "cron" || got.Matches[1].Fixed != "1:2.0" {
		t.Fatalf("epoch: %+v", got.Matches[1])
	}
	inv.Packages[0].Version = "3.0.13-0ubuntu3.4"
	inv.Packages[1].SourceVersion = "2.39-0ubuntu8.4"
	inv.Packages[3].Version = "1:2.0"
	got = matchAdvisories(inv, path)
	if len(got.Matches) != 0 {
		t.Fatalf("backport, fixed source and epoch must not match: %+v", got.Matches)
	}
}

func TestMatchAdvisoriesKeepsInventoryWarning(t *testing.T) {
	path := writeFeed(t, `{"schema":1,"distro":"ubuntu","release":"24.04","advisories":[]}`)
	inv := proto.PackageInventory{Distro: "ubuntu", Release: "24.04", Gaps: []string{"dpkg-query"}}
	got := matchAdvisories(inv, path)
	if got.Feed != "imported" || !containsText(got.Coverage, "incomplete") {
		t.Fatalf("the inventory gap must survive a loaded feed: %+v", got)
	}
}

func writeFeed(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "feed.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func containsText(s, sub string) bool { return strings.Contains(s, sub) }

func TestExposureStale(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	if (Exposure{CheckedAt: now.Add(-90 * time.Minute)}).Stale(now) {
		t.Fatal("90 minutes is within two intervals")
	}
	if !(Exposure{CheckedAt: now.Add(-3 * time.Hour)}).Stale(now) {
		t.Fatal("3 hours old must be stale")
	}
	if !(Exposure{}).Stale(now) {
		t.Fatal("never collected must be stale")
	}
}
