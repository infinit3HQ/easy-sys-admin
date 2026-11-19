package fleet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

func TestMatchAdvisoriesDoesNotCallAMissClean(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feed.json")
	feed := []byte(`{"schema":1,"distro":"ubuntu","release":"24.04","advisories":[{"id":"USN-1","package":"openssl","fixed":"3.0.13-0ubuntu3.2","severity":"high","affected":["3.0.13-0ubuntu3.1"]}]}`)
	if err := os.WriteFile(path, feed, 0o644); err != nil {
		t.Fatal(err)
	}
	inv := proto.PackageInventory{Distro: "ubuntu", Release: "24.04", Packages: []proto.InstalledPackage{
		{Name: "openssl", Version: "3.0.13-0ubuntu3.1"},
		{Name: "bash", Version: "5.2.21-2ubuntu4"},
	}}
	got := matchAdvisories(inv, path)
	if len(got.Matches) != 1 || got.Matches[0].Package != "openssl" {
		t.Fatalf("matches: %+v", got.Matches)
	}
	if got.Packages != 2 {
		t.Fatalf("bash must stay in the inventory count, not be dropped as safe: %+v", got)
	}
	other := matchAdvisories(proto.PackageInventory{Distro: "debian", Release: "12", Packages: inv.Packages}, path)
	if len(other.Matches) != 0 || other.Feed != "imported" {
		t.Fatalf("another release must not inherit the matches: %+v", other)
	}
	none := matchAdvisories(inv, "")
	if none.Feed != "not imported" || len(none.Matches) != 0 {
		t.Fatalf("no feed: %+v", none)
	}
}
