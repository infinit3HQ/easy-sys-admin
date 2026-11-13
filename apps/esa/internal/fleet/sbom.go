package fleet

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/version"
)

// SBOM is a CycloneDX 1.5 bill of materials of one server's installed deb
// packages. A scanner run by the operator, offline if they choose, reads
// it; esa sends it nowhere.
type SBOM struct {
	BOMFormat    string          `json:"bomFormat"`
	SpecVersion  string          `json:"specVersion"`
	SerialNumber string          `json:"serialNumber"`
	Version      int             `json:"version"`
	Metadata     SBOMMetadata    `json:"metadata"`
	Components   []SBOMComponent `json:"components"`
}

// SBOMMetadata names the server and when the inventory was read.
type SBOMMetadata struct {
	Timestamp string        `json:"timestamp"`
	Tools     []SBOMTool    `json:"tools"`
	Component SBOMComponent `json:"component"`
}

// SBOMTool is the producer.
type SBOMTool struct {
	Vendor  string `json:"vendor"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// SBOMComponent is one package, or the operating system.
type SBOMComponent struct {
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Version    string         `json:"version"`
	Purl       string         `json:"purl,omitempty"`
	BOMRef     string         `json:"bom-ref,omitempty"`
	Properties []SBOMProperty `json:"properties,omitempty"`
}

// SBOMProperty is a name and value.
type SBOMProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ServerSBOM reads the installed packages from the helper and returns them
// as CycloneDX. It needs a helper with the inventory action, and fails
// rather than returning a partial list when dpkg cannot be read.
func (s *Service) ServerSBOM(ctx context.Context, a Actor, serverID int64) (SBOM, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return SBOM{}, err
	}
	srv, _, err := s.loadVisibleServer(ctx, a, serverID)
	if err != nil {
		return SBOM{}, err
	}
	var inv proto.PackageInventory
	if err := s.callServer(ctx, srv, time.Minute, &inv, "inventory"); err != nil {
		return SBOM{}, err
	}
	if len(inv.Gaps) > 0 {
		return SBOM{}, &UnavailableError{Message: srv.Name + ": the package list is incomplete (" + strings.Join(inv.Gaps, ", ") + "), so no SBOM was made"}
	}
	return buildSBOM(srv.Name, inv, s.now(), version.Version), nil
}

func buildSBOM(server string, inv proto.PackageInventory, now time.Time, version string) SBOM {
	distro := strings.ToLower(inv.Distro)
	bom := SBOM{
		BOMFormat: "CycloneDX", SpecVersion: "1.5", SerialNumber: "urn:uuid:" + uuid4(), Version: 1,
		Metadata: SBOMMetadata{
			Timestamp: now.UTC().Format(time.RFC3339),
			Tools:     []SBOMTool{{Vendor: "easy-sys-admin", Name: "esa", Version: version}},
			Component: SBOMComponent{Type: "operating-system", Name: distro, Version: inv.Release, BOMRef: "os",
				Properties: []SBOMProperty{{Name: "esa:server", Value: server}}},
		},
		Components: make([]SBOMComponent, 0, len(inv.Packages)+1),
	}
	bom.Components = append(bom.Components, SBOMComponent{Type: "operating-system", Name: distro, Version: inv.Release, BOMRef: "os-" + distro})
	for _, p := range inv.Packages {
		bom.Components = append(bom.Components, SBOMComponent{
			Type: "library", Name: p.Name, Version: p.Version, Purl: debPurl(distro, inv.Release, p), BOMRef: p.Name + ":" + p.Arch,
		})
	}
	return bom
}

// debPurl is the package URL scanners use to match deb packages, with the
// source package as "upstream" when it differs.
func debPurl(distro, release string, p proto.InstalledPackage) string {
	q := url.Values{}
	q.Set("arch", p.Arch)
	q.Set("distro", distro+"-"+release)
	if p.Source != "" && (p.Source != p.Name || (p.SourceVersion != "" && p.SourceVersion != p.Version)) {
		up := p.Source
		if p.SourceVersion != "" && p.SourceVersion != p.Version {
			up += "@" + p.SourceVersion
		}
		q.Set("upstream", up)
	}
	return fmt.Sprintf("pkg:deb/%s/%s@%s?%s", url.PathEscape(distro), url.PathEscape(p.Name), url.PathEscape(p.Version), q.Encode())
}

func uuid4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
