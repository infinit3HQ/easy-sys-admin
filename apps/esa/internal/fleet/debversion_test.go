package fleet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
