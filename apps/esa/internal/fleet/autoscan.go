package fleet

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const (
	// scanEvery is how old a server's last scan may be before AutoScan
	// scans it again.
	scanEvery = 20 * time.Hour
	// scanDBEvery is how often the scanner's database is refreshed.
	scanDBEvery = 20 * time.Hour
	// scansPerRun caps scans per hourly run.
	scansPerRun = 50
)

// scannerEnv turns off everything in Grype that talks to the network
// during a scan: its own version check and database auto-update. Only the
// explicit "db update" step downloads, and it sends no server data.
func scannerEnv() []string {
	return append(os.Environ(), "GRYPE_CHECK_FOR_APP_UPDATE=false", "GRYPE_DB_AUTO_UPDATE=false")
}

// AutoScan refreshes the scanner's database once a day and scans servers
// whose last scan is old. It does nothing unless ESA_SCANNER is set. The
// SBOM is written to a private temporary file and read by the local
// scanner; it is not sent anywhere.
func (s *Service) AutoScan(ctx context.Context) {
	if s.scanner == "" {
		return
	}
	if s.now().Sub(s.scanDBAt) > scanDBEvery {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		cmd := exec.CommandContext(cctx, s.scanner, "db", "update")
		cmd.Env = scannerEnv()
		out, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			s.log.Warn("scanner database update failed; scans keep the old database and show as stale", "err", err, "output", trimRunes(string(out), 300))
		} else {
			s.scanDBAt = s.now()
		}
	}
	var servers []store.Server
	if err := s.db.WithContext(ctx).Where("enrollment = ?", "ready").Order("id").Find(&servers).Error; err != nil {
		s.log.Error("list servers to scan", "err", err)
		return
	}
	done := 0
	for _, srv := range servers {
		if done == scansPerRun || ctx.Err() != nil {
			return
		}
		if !proto.Supports(storedCapabilities(srv), "inventory") {
			continue
		}
		var last store.ScanImport
		if s.db.WithContext(ctx).Select("imported_at").Where("server_id = ?", srv.ID).Order("id desc").Take(&last).Error == nil &&
			s.now().Sub(last.ImportedAt) < scanEvery {
			continue
		}
		done++
		if err := s.scanServer(ctx, srv); err != nil {
			s.log.Warn("scan", "server_id", srv.ID, "err", err)
		}
	}
}

func (s *Service) scanServer(ctx context.Context, srv store.Server) error {
	var inv proto.PackageInventory
	if err := s.callServer(ctx, srv, time.Minute, &inv, "inventory"); err != nil {
		return err
	}
	if len(inv.Gaps) > 0 {
		return &UnavailableError{Message: "incomplete package list"}
	}
	raw, err := json.Marshal(buildSBOM(srv.Name, inv, s.now(), "esa"))
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "esa-sbom-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, s.scanner, "sbom:"+f.Name(), "-o", "json", "-q")
	cmd.Env = scannerEnv()
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	scanner, ver, built, matches, err := parseGrype(out)
	if err != nil {
		return err
	}
	if matches == nil {
		matches = []VulnMatch{}
	}
	row := store.ScanImport{AccountID: srv.AccountID, ServerID: srv.ID, Scanner: scanner, ScannerVersion: ver, DBBuilt: built,
		Matches: store.MustJSON(matches), ImportedAt: s.now()}
	return s.db.WithContext(ctx).Omit("Account", "Server").Create(&row).Error
}
