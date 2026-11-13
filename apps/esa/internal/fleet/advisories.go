package fleet

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// AdvisoryMatch is one package whose installed version is listed as affected.
// A package that is absent from the feed is not certified safe.
type AdvisoryMatch struct {
	ID       string `json:"id"`
	Package  string `json:"package"`
	Version  string `json:"version"`
	Fixed    string `json:"fixed"`
	Severity string `json:"severity"`
}

// AdvisoryReport is the result of an offline exact-version match.
type AdvisoryReport struct {
	Coverage string          `json:"coverage"`
	Distro   string          `json:"distro"`
	Release  string          `json:"release"`
	Packages int             `json:"packages"`
	Feed     string          `json:"feed"`
	Matches  []AdvisoryMatch `json:"matches"`
}

type advisoryFeed struct {
	Schema     int    `json:"schema"`
	Distro     string `json:"distro"`
	Release    string `json:"release"`
	Advisories []struct {
		ID       string   `json:"id"`
		Package  string   `json:"package"`
		Fixed    string   `json:"fixed"`
		Severity string   `json:"severity"`
		Affected []string `json:"affected"`
	} `json:"advisories"`
}

// Advisories reads the installed packages and matches them to the local feed.
// It does not download anything.
func (s *Service) Advisories(ctx context.Context, a Actor, serverID int64) (AdvisoryReport, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return AdvisoryReport{}, err
	}
	srv, _, err := s.loadVisibleServer(ctx, a, serverID)
	if err != nil {
		return AdvisoryReport{}, err
	}
	var inv proto.PackageInventory
	if err := s.callServer(ctx, srv, time.Minute, &inv, "inventory"); err != nil {
		return AdvisoryReport{}, err
	}
	rep := matchAdvisories(inv, s.advisoryFeed)
	return rep, nil
}

func matchAdvisories(inv proto.PackageInventory, path string) AdvisoryReport {
	rep := matchFeed(inv, path)
	// A failed or partial inventory stays visible whatever the feed says,
	// so zero matches never reads as a clean result.
	if len(inv.Gaps) > 0 {
		rep.Coverage = "Package inventory is incomplete (" + strings.Join(inv.Gaps, ", ") + "). Matches below are not a full result. " + rep.Coverage
	}
	return rep
}

func matchFeed(inv proto.PackageInventory, path string) AdvisoryReport {
	rep := AdvisoryReport{
		Distro: inv.Distro, Release: inv.Release, Packages: len(inv.Packages),
		Feed: "not imported", Matches: []AdvisoryMatch{},
		Coverage: "No advisory feed is imported. A missing list is not a clean result.",
	}
	if path == "" {
		return rep
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		rep.Feed = "unreadable"
		rep.Coverage = "The advisory feed could not be read. That is not a clean result."
		return rep
	}
	var feed advisoryFeed
	if err := json.Unmarshal(raw, &feed); err != nil || feed.Schema != 1 {
		rep.Feed = "unreadable"
		rep.Coverage = "The advisory feed schema is not 1. That is not a clean result."
		return rep
	}
	rep.Feed = "imported"
	if feed.Distro != inv.Distro || feed.Release != inv.Release {
		rep.Coverage = "The feed is for " + feed.Distro + " " + feed.Release + ", and this server is " + inv.Distro + " " + inv.Release + ". No match from another release is treated as safe."
		return rep
	}
	byPackage := map[string][]int{}
	for i, a := range feed.Advisories {
		byPackage[a.Package] = append(byPackage[a.Package], i)
	}
	for _, p := range inv.Packages {
		for _, i := range byPackage[p.Name] {
			a := feed.Advisories[i]
			if !affectedVersion(p.Version, a.Fixed, a.Affected) {
				continue
			}
			rep.Matches = append(rep.Matches, AdvisoryMatch{ID: a.ID, Package: p.Name, Version: p.Version, Fixed: a.Fixed, Severity: a.Severity})
		}
	}
	rep.Coverage = "Installed versions older than the fixed version, or listed as affected, in the imported feed are shown. A package that is absent is not certified safe."
	return rep
}

// affectedVersion is true when installed is listed as affected, or is
// older than fixed by Debian version order.
func affectedVersion(installed, fixed string, affected []string) bool {
	if slices.Contains(affected, installed) {
		return true
	}
	return fixed != "" && compareDebVersions(installed, fixed) < 0
}
