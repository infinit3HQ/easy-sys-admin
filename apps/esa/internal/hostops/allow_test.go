package hostops

import (
	"slices"
	"testing"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

func TestChooseUpgradesKeepsToTheApprovedList(t *testing.T) {
	pending := []proto.PendingPackage{
		{Name: "openssl", Security: true},
		{Name: "bash", Security: true},
		{Name: "curl", Security: true},
		{Name: "vim"},
	}
	o := UpgradeOptions{Mode: proto.UpdateSecurity, Hold: []string{"curl"}, Allow: []string{"openssl", "curl"}}
	install, skipped, notApproved := chooseUpgrades(pending, o)
	if !slices.Equal(install, []string{"openssl"}) || !slices.Equal(skipped, []string{"curl"}) || !slices.Equal(notApproved, []string{"bash"}) {
		t.Fatalf("install %v skipped %v not approved %v", install, skipped, notApproved)
	}
	none, _, _ := chooseUpgrades(pending, UpgradeOptions{Mode: proto.UpdateAll, Allow: []string{}})
	if len(none) != 0 {
		t.Fatalf("an empty approved list installed %v", none)
	}
	all, _, _ := chooseUpgrades(pending, UpgradeOptions{Mode: proto.UpdateAll})
	if len(all) != 4 {
		t.Fatalf("without a list every pending package is chosen: %v", all)
	}
}

func TestUnapprovedDependencyStopsThePlan(t *testing.T) {
	sim := []proto.PendingPackage{{Name: "openssl"}, {Name: "libssl3"}}
	o := UpgradeOptions{Allow: []string{"openssl"}}
	if got := unapproved(sim, o); !slices.Equal(got, []string{"libssl3"}) {
		t.Fatalf("got %v", got)
	}
	o.Allow = append(o.Allow, "libssl3")
	o.Hold = []string{"libssl3"}
	if got := unapproved(sim, o); !slices.Equal(got, []string{"libssl3"}) {
		t.Fatalf("a held dependency must stop the plan: %v", got)
	}
}

func TestParseDpkgVersions(t *testing.T) {
	got := parseDpkgVersions("libsqlite3-0:arm64\t3.45.1-1ubuntu2.3\nlocales\t2.39-0ubuntu8.6\n\nbroken line\n")
	if got["libsqlite3-0"] != "3.45.1-1ubuntu2.3" || got["locales"] != "2.39-0ubuntu8.6" || len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}
