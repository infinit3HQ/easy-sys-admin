package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const (
	// maxScanBytes caps an imported scan result.
	maxScanBytes = 10 << 20
	// maxScanMatches caps stored matches per import. A real Ubuntu server
	// with an old kernel still installed gave 14,384 matches.
	maxScanMatches = 20000
	// scanFeedMaxAge is how old the scanner's database, or the scan, may
	// be before the result is shown as stale: a daily update with a day of
	// slack. New advisories are published every day.
	scanFeedMaxAge = 48 * time.Hour
)

// VulnMatch is one advisory for one installed package, from a scanner.
type VulnMatch struct {
	ID             string   `json:"id"`
	Aliases        []string `json:"aliases,omitempty"`
	Package        string   `json:"package"`
	Version        string   `json:"version"`
	Severity       string   `json:"severity"`
	FixState       string   `json:"fixState"`
	FixedIn        []string `json:"fixedIn,omitempty"`
	KnownExploited bool     `json:"knownExploited,omitempty"`
	EPSS           float64  `json:"epss,omitempty"`
	// ImageDigest is the image this match was found in. Empty means the
	// report did not give one. ImageTag is the name the report used.
	// A later report that only changes the tag does not drop the match.
	ImageDigest string `json:"imageDigest,omitempty"`
	ImageTag    string `json:"imageTag,omitempty"`
	// NotRunningKernel is a kernel package that is installed but is not the
	// kernel the server is running. The match is kept: the package is still
	// on disk.
	NotRunningKernel bool `json:"notRunningKernel,omitempty"`
	// ExceptedUntil is set when a person has set this match aside until that
	// date. The match stays in the list. An exception is not a fix.
	ExceptedUntil *time.Time `json:"exceptedUntil,omitempty"`
	// Tier ranks the match: 1 known exploited, 2 a fix exists for a high
	// or critical issue, 3 everything else. It explains, not scores.
	Tier   int    `json:"tier"`
	Reason string `json:"reason"`
}

// ScanView is the latest imported scan of a server.
type ScanView struct {
	Imported       bool
	Scanner        string
	ScannerVersion string
	DBBuilt        *time.Time
	ImportedAt     time.Time
	Stale          bool
	Coverage       string
	RunningKernel  string
	Matches        []VulnMatch
}

// grypeReport is the part of Grype's JSON output esa reads.
type grypeReport struct {
	Matches []struct {
		Vulnerability struct {
			ID       string `json:"id"`
			Severity string `json:"severity"`
			Fix      struct {
				Versions []string `json:"versions"`
				State    string   `json:"state"`
			} `json:"fix"`
			KnownExploited []json.RawMessage `json:"knownExploited"`
			EPSS           []struct {
				EPSS float64 `json:"epss"`
			} `json:"epss"`
		} `json:"vulnerability"`
		RelatedVulnerabilities []struct {
			ID string `json:"id"`
		} `json:"relatedVulnerabilities"`
		Artifact struct {
			Name     string `json:"name"`
			Version  string `json:"version"`
			Type     string `json:"type"`
			Metadata struct {
				ImageID   string `json:"imageID"`
				UserInput string `json:"userInput"`
			} `json:"metadata"`
		} `json:"artifact"`
	} `json:"matches"`
	Descriptor struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		DB      struct {
			Built  string `json:"built"`
			Status struct {
				Built string `json:"built"`
			} `json:"status"`
		} `json:"db"`
	} `json:"descriptor"`
}

// parseGrype normalises Grype JSON. Aliases of one advisory for one
// package become one match, so one fix is not counted several times.
func parseGrype(raw []byte) (scanner, version string, built *time.Time, matches []VulnMatch, err error) {
	var r grypeReport
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", "", nil, nil, invalid("scan", "not a Grype JSON report: %s", err.Error())
	}
	if !strings.EqualFold(r.Descriptor.Name, "grype") {
		return "", "", nil, nil, invalid("scan", "the report does not say it came from Grype")
	}
	for _, s := range []string{r.Descriptor.DB.Built, r.Descriptor.DB.Status.Built} {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			built = &t
			break
		}
	}
	byKey := map[string]int{}
	for _, m := range r.Matches {
		v := m.Vulnerability
		if v.ID == "" || m.Artifact.Name == "" {
			continue
		}
		aliases := []string{}
		for _, rel := range m.RelatedVulnerabilities {
			if rel.ID != "" && rel.ID != v.ID {
				aliases = append(aliases, rel.ID)
			}
		}
		key := m.Artifact.Name + "\x00" + v.ID
		for _, al := range aliases {
			if i, ok := byKey[m.Artifact.Name+"\x00"+al]; ok {
				byKey[key] = i
			}
		}
		vm := VulnMatch{
			ID: v.ID, Aliases: aliases, Package: m.Artifact.Name, Version: m.Artifact.Version,
			Severity: strings.ToLower(v.Severity), FixState: v.Fix.State, FixedIn: v.Fix.Versions,
			KnownExploited: len(v.KnownExploited) > 0,
			ImageDigest:    m.Artifact.Metadata.ImageID,
			ImageTag:       m.Artifact.Metadata.UserInput,
		}
		for _, e := range v.EPSS {
			vm.EPSS = max(vm.EPSS, e.EPSS)
		}
		if i, ok := byKey[key]; ok {
			matches[i].Aliases = slices.DeleteFunc(mergeNames(matches[i].Aliases, append(aliases, v.ID)), func(n string) bool { return n == matches[i].ID })
			matches[i].KnownExploited = matches[i].KnownExploited || vm.KnownExploited
			matches[i].EPSS = max(matches[i].EPSS, vm.EPSS)
			continue
		}
		if len(matches) == maxScanMatches {
			return "", "", nil, nil, invalid("scan", "more than %d matches; import a smaller report", maxScanMatches)
		}
		byKey[key] = len(matches)
		for _, al := range aliases {
			byKey[m.Artifact.Name+"\x00"+al] = len(matches)
		}
		matches = append(matches, vm)
	}
	for i := range matches {
		matches[i].Tier, matches[i].Reason = rankMatch(matches[i])
	}
	sortMatches(matches)
	return "grype", trimRunes(r.Descriptor.Version, 40), built, matches, nil
}

func mergeNames(a, b []string) []string {
	out := slices.Clone(a)
	for _, n := range b {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

var severityRank = map[string]int{"critical": 4, "high": 3, "medium": 2, "low": 1, "negligible": 0}

// rankMatch explains a match's place. Upstream severity is kept as is;
// nothing here makes a score up.
func rankMatch(m VulnMatch) (int, string) {
	fixed := m.FixState == "fixed" && len(m.FixedIn) > 0
	switch {
	case m.KnownExploited:
		return 1, "known to be exploited"
	case fixed && severityRank[m.Severity] >= 3:
		return 2, m.Severity + " with a fix available"
	case fixed:
		return 3, "a fix is available"
	case m.FixState == "wont-fix":
		return 3, "the distribution will not fix it"
	default:
		return 3, "no fix published yet"
	}
}

func sortMatches(ms []VulnMatch) {
	slices.SortStableFunc(ms, func(a, b VulnMatch) int {
		if a.NotRunningKernel != b.NotRunningKernel {
			if a.NotRunningKernel {
				return 1
			}
			return -1
		}
		if a.Tier != b.Tier {
			return a.Tier - b.Tier
		}
		if d := severityRank[b.Severity] - severityRank[a.Severity]; d != 0 {
			return d
		}
		switch {
		case a.EPSS > b.EPSS:
			return -1
		case a.EPSS < b.EPSS:
			return 1
		}
		return strings.Compare(a.Package+a.ID, b.Package+b.ID)
	})
}

// ImportScan stores a scanner's result for a server. esa does not run the
// scanner; it keeps what the report says, with its tool and database age.
func (s *Service) ImportScan(ctx context.Context, a Actor, serverID int64, raw []byte) (ScanView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return ScanView{}, err
	}
	if len(raw) > maxScanBytes {
		return ScanView{}, invalid("scan", "the report is larger than %d MiB", maxScanBytes>>20)
	}
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return ScanView{}, err
	}
	scanner, ver, built, matches, err := parseGrype(raw)
	if err != nil {
		return ScanView{}, err
	}
	if matches == nil {
		matches = []VulnMatch{}
	}
	var prev []VulnMatch
	var old store.ScanImport
	if err := s.db.WithContext(ctx).Where("server_id = ?", srv.ID).Order("imported_at desc, id desc").Take(&old).Error; err == nil {
		_ = old.Matches.Decode(&prev)
	}
	var state store.ServerState
	_ = s.db.WithContext(ctx).Select("images_read", "image_digests").Where("server_id = ?", srv.ID).Take(&state).Error
	var digests []string
	_ = state.ImageDigests.Decode(&digests)
	kept := 0
	matches, kept = retainUnverifiedImages(prev, matches, state.ImagesRead, digests)
	row := store.ScanImport{AccountID: srv.AccountID, ServerID: srv.ID, Scanner: scanner, ScannerVersion: ver, DBBuilt: built,
		Matches: store.MustJSON(matches), ImportedAt: s.now(), ImportedBy: a.UserID}
	if err := s.db.WithContext(ctx).Omit("Account", "Server").Create(&row).Error; err != nil {
		return ScanView{}, err
	}
	s.audit(ctx, nil, a, "scan.import", srv.Name, map[string]any{"scanner": scanner, "version": ver, "matches": len(matches), "keptImages": kept})
	return s.LatestScan(ctx, a, serverID)
}

// LatestScan returns the newest imported scan of a server, or an empty
// view that says none was imported. No import is not a clean result.
func (s *Service) LatestScan(ctx context.Context, a Actor, serverID int64) (ScanView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return ScanView{}, err
	}
	srv, _, err := s.loadVisibleServer(ctx, a, serverID)
	if err != nil {
		return ScanView{}, err
	}
	var row store.ScanImport
	err = s.db.WithContext(ctx).Where("server_id = ?", srv.ID).Order("id desc").Take(&row).Error
	if err != nil {
		return ScanView{Matches: []VulnMatch{}, Stale: true,
			Coverage: "No scan was imported for this server. That is not a clean result."}, nil
	}
	v := ScanView{Imported: true, Scanner: row.Scanner, ScannerVersion: row.ScannerVersion, DBBuilt: row.DBBuilt, ImportedAt: row.ImportedAt}
	_ = row.Matches.Decode(&v.Matches)
	if v.Matches == nil {
		v.Matches = []VulnMatch{}
	}
	now := s.now()
	switch {
	case row.DBBuilt == nil:
		v.Stale, v.Coverage = true, "The report does not say how old its vulnerability database is, so it is treated as stale."
	case now.Sub(*row.DBBuilt) > scanFeedMaxAge:
		v.Stale, v.Coverage = true, "The scanner's database was built "+row.DBBuilt.UTC().Format("Jan 2")+", more than 2 days ago. Newer advisories are missing."
	default:
		v.Coverage = "Matches from " + row.Scanner + " for the packages in the SBOM it scanned. A package without a match is not certified safe."
	}
	if now.Sub(row.ImportedAt) > scanFeedMaxAge {
		v.Stale = true
		v.Coverage += " The scan is from " + row.ImportedAt.UTC().Format("Jan 2") + "; the packages may have changed since."
	}
	v.RunningKernel = s.runningKernel(ctx, srv.ID)
	idle := markIdleKernels(v.Matches, v.RunningKernel)
	excepted := s.markExceptions(ctx, srv.ID, v.Matches)
	sortMatches(v.Matches)
	if excepted > 0 {
		v.Coverage += fmt.Sprintf(" %d matches are excepted until a date. An exception is not a fix.", excepted)
	}
	for _, m := range v.Matches {
		if m.ImageDigest != "" || m.ImageTag != "" {
			v.Coverage += " An image match stays until a read of the local images shows that digest is gone. A tag change does not clear it."
			break
		}
	}
	switch {
	case idle == 1:
		v.Coverage += " 1 match is a kernel package that is installed but not the running kernel " + v.RunningKernel + ". It stays in the list, after the others, because that package is still on disk."
	case idle > 1:
		v.Coverage += fmt.Sprintf(" %d matches are kernel packages that are installed but not the running kernel %s. They stay in the list, after the others, because those packages are still on disk.", idle, v.RunningKernel)
	case v.RunningKernel == "" && kernelPackages(v.Matches) > 0:
		v.Coverage += " The running kernel is not known yet, so installed kernels are not separated from the packages in use."
	}
	return v, nil
}

func (s *Service) runningKernel(ctx context.Context, serverID int64) string {
	var st store.ServerState
	if err := s.db.WithContext(ctx).Where("server_id = ?", serverID).Take(&st).Error; err != nil {
		return ""
	}
	var snap struct {
		Kernel string `json:"kernel"`
	}
	if st.Snapshot.Decode(&snap) != nil {
		return ""
	}
	return strings.TrimSpace(snap.Kernel)
}

// kernelPkgRelease matches a kernel image, modules, or headers package
// whose name includes the uname release, such as
// linux-modules-7.0.0-1013-generic. Meta packages such as
// linux-image-generic do not match.
var kernelPkgRelease = regexp.MustCompile(`^linux-(?:image-unsigned|modules-extra|cloud-tools|modules|headers|image|tools)-(\d+\.\d+\.\d+-\d+(?:-[a-z0-9]+)?)(?:-dbgsym)?$`)

func kernelRelease(pkg string) string {
	m := kernelPkgRelease.FindStringSubmatch(pkg)
	if m == nil {
		return ""
	}
	return m[1]
}

func sameKernel(pkgRelease, running string) bool {
	if pkgRelease == running {
		return true
	}
	return strings.HasPrefix(running, pkgRelease+"-") || strings.HasPrefix(pkgRelease, running+"-")
}

// markIdleKernels flags matches for a kernel build that is not running.
// An unknown running kernel flags nothing. It returns how many it flagged.
func markIdleKernels(ms []VulnMatch, running string) int {
	if running == "" {
		return 0
	}
	n := 0
	for i := range ms {
		rel := kernelRelease(ms[i].Package)
		if rel == "" || sameKernel(rel, running) {
			continue
		}
		ms[i].NotRunningKernel = true
		n++
	}
	return n
}

func kernelPackages(ms []VulnMatch) int {
	n := 0
	for _, m := range ms {
		if kernelRelease(m.Package) != "" {
			n++
		}
	}
	return n
}

// retainUnverifiedImages keeps an image finding that a new report dropped
// when the drop is not backed by a local image list. A tag change, a missing
// digest, or an unread image list does not clear it. A digest that the last
// local list does not contain is not kept.
func retainUnverifiedImages(prev, next []VulnMatch, imagesRead bool, digests []string) ([]VulnMatch, int) {
	have := map[string]bool{}
	for _, m := range next {
		have[m.Package+"\n"+m.ID] = true
	}
	present := map[string]bool{}
	for _, d := range digests {
		present[d] = true
	}
	n := 0
	for _, m := range prev {
		if m.ImageDigest == "" && m.ImageTag == "" {
			continue
		}
		if have[m.Package+"\n"+m.ID] {
			continue
		}
		if m.ImageDigest != "" && imagesRead && !present[m.ImageDigest] {
			continue
		}
		if len(next) >= maxScanMatches {
			break
		}
		next = append(next, m)
		n++
	}
	return next, n
}
