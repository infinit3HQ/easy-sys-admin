package fleet

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

var advisoryIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._-]{2,39}$`)
var debNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]{0,79}$`)

type VulnExceptionView struct {
	ID         int64
	ServerID   int64
	AdvisoryID string
	Package    string
	Until      time.Time
	Reason     string
}

func (s *Service) CreateVulnException(ctx context.Context, a Actor, serverID int64, advisory, pkg, reason string, until time.Time) (VulnExceptionView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return VulnExceptionView{}, err
	}
	advisory = strings.TrimSpace(advisory)
	pkg = strings.TrimSpace(pkg)
	reason = strings.TrimSpace(reason)
	if !advisoryIDRe.MatchString(advisory) {
		return VulnExceptionView{}, invalid("advisory", "use the advisory id from the scan, up to 40 characters")
	}
	if !debNameRe.MatchString(pkg) {
		return VulnExceptionView{}, invalid("package", "use the package name from the scan")
	}
	if reason == "" || utf8.RuneCountInString(reason) > 200 {
		return VulnExceptionView{}, invalid("reason", "say why, in at most 200 characters")
	}
	now := s.now()
	if !until.After(now) || until.After(now.Add(90*24*time.Hour)) {
		return VulnExceptionView{}, invalid("until", "must be within the next 90 days")
	}
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return VulnExceptionView{}, err
	}
	var row store.VulnException
	err = s.db.WithContext(ctx).Where("server_id = ? AND advisory_id = ? AND package = ?", srv.ID, advisory, pkg).Take(&row).Error
	if err == nil {
		if err := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{"until": until, "reason": reason}).Error; err != nil {
			return VulnExceptionView{}, err
		}
		row.Until, row.Reason = until, reason
	} else {
		row = store.VulnException{AccountID: srv.AccountID, ServerID: srv.ID, AdvisoryID: advisory, Package: pkg, Until: until, Reason: reason, CreatedAt: now}
		if err := s.db.WithContext(ctx).Omit("Account", "Server").Create(&row).Error; err != nil {
			return VulnExceptionView{}, err
		}
	}
	s.audit(ctx, nil, a, "vuln.exception", srv.Name, map[string]any{"advisory": advisory, "package": pkg, "until": until})
	return VulnExceptionView{ID: row.ID, ServerID: row.ServerID, AdvisoryID: row.AdvisoryID, Package: row.Package, Until: row.Until, Reason: row.Reason}, nil
}

func (s *Service) DeleteVulnException(ctx context.Context, a Actor, serverID, id int64) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Where("id = ? AND server_id = ?", id, srv.ID).Delete(&store.VulnException{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	s.audit(ctx, nil, a, "vuln.exception.delete", srv.Name, map[string]any{"id": id})
	return nil
}

// markExceptions sets ExceptedUntil on matches that have a current exception.
// Expired exceptions do not apply. The match is not removed.
func (s *Service) markExceptions(ctx context.Context, serverID int64, matches []VulnMatch) int {
	var rows []store.VulnException
	if err := s.db.WithContext(ctx).Where("server_id = ? AND until > ?", serverID, s.now()).Find(&rows).Error; err != nil {
		return 0
	}
	by := map[string]time.Time{}
	for _, r := range rows {
		by[r.Package+"\n"+r.AdvisoryID] = r.Until
	}
	n := 0
	for i := range matches {
		if until, ok := by[matches[i].Package+"\n"+matches[i].ID]; ok {
			t := until
			matches[i].ExceptedUntil = &t
			n++
		}
	}
	return n
}
