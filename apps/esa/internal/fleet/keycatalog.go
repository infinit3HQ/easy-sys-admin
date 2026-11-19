package fleet

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// KeyOccurrenceView is one login on one server that has a fingerprint.
type KeyOccurrenceView struct {
	ServerID   int64
	ServerName string
	User       string
	Comment    string
	From       string
	// HasCommand is this login's restriction. The same key can be
	// unrestricted on another login.
	HasCommand bool
	ObservedAt time.Time
}

// KeyView is one fingerprint across the servers the actor can see.
type KeyView struct {
	Fingerprint string
	KeyType     string
	HasCommand  bool
	Label       string
	Uses        []KeyOccurrenceView
}

var fingerprintRe = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{20,80}={0,2}$`)

// syncKeyOccurrences replaces the stored keys for one server. Call it only
// after a helper new enough to read the standard authorized_keys files.
func (s *Service) syncKeyOccurrences(ctx context.Context, srv store.Server, keys []keyUse, at time.Time, complete bool) {
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		s.log.Error("sync keys", "err", tx.Error)
		return
	}
	if err := tx.Where("server_id = ?", srv.ID).Delete(&store.KeyOccurrence{}).Error; err != nil {
		tx.Rollback()
		s.log.Error("sync keys", "err", err)
		return
	}
	for _, k := range keys {
		row := store.KeyOccurrence{
			AccountID: srv.AccountID, ServerID: srv.ID, User: k.User, Fingerprint: k.Fingerprint,
			KeyType: k.Type, Comment: k.Comment, From: k.From, HasCommand: k.HasCommand, ObservedAt: at,
		}
		if err := tx.Omit("Account", "Server").Create(&row).Error; err != nil {
			tx.Rollback()
			s.log.Error("sync keys", "err", err)
			return
		}
	}
	if err := tx.Commit().Error; err != nil {
		s.log.Error("sync keys", "err", err)
		return
	}
	s.reportKeyDrift(ctx, srv, keys, complete)
}

type baselineKey struct {
	User        string `json:"user"`
	Fingerprint string `json:"fingerprint"`
	From        string `json:"from"`
	HasCommand  bool   `json:"hasCommand"`
}

func (s *Service) reportKeyDrift(ctx context.Context, srv store.Server, keys []keyUse, complete bool) {
	var base store.KeyBaseline
	if err := s.db.WithContext(ctx).Where("server_id = ?", srv.ID).Take(&base).Error; err != nil {
		return
	}
	var approved []baselineKey
	_ = base.Items.Decode(&approved)
	have := map[string]baselineKey{}
	for _, item := range approved {
		have[item.User+"\n"+item.Fingerprint] = item
	}
	seen := map[string]bool{}
	excepted := s.keyExceptions(ctx, srv.ID)
	for _, k := range keys {
		id := k.User + "\n" + k.Fingerprint
		seen[id] = true
		old, ok := have[id]
		if !ok {
			if excepted[id] {
				continue
			}
			s.openIncident(ctx, srv.AccountID, srv.ID, "new_key", k.Fingerprint+" on "+k.User, srv.Name+": key "+k.Fingerprint+" on "+k.User+" is not in the approved baseline", false)
			continue
		}
		if old.From != k.From || old.HasCommand != k.HasCommand {
			s.openIncident(ctx, srv.AccountID, srv.ID, "new_key", k.Fingerprint+" on "+k.User+" changed", srv.Name+": restrictions changed for "+k.Fingerprint+" on "+k.User, false)
		}
	}
	for id, item := range have {
		if seen[id] {
			continue
		}
		s.openIncident(ctx, srv.AccountID, srv.ID, "new_key", "removed "+item.Fingerprint+" on "+item.User, srv.Name+": approved key "+item.Fingerprint+" on "+item.User+" was not in the files checked this time", false)
	}
	if !complete {
		return
	}
	open := map[string]bool{}
	for _, k := range keys {
		id := k.User + "\n" + k.Fingerprint
		if _, ok := have[id]; !ok && excepted[id] {
			continue
		}
		if _, ok := have[id]; !ok {
			open[k.Fingerprint+" on "+k.User] = true
			continue
		}
		old := have[id]
		if old.From != k.From || old.HasCommand != k.HasCommand {
			open[k.Fingerprint+" on "+k.User+" changed"] = true
		}
	}
	for id, item := range have {
		if !seen[id] {
			open["removed "+item.Fingerprint+" on "+item.User] = true
		}
	}
	var rows []store.SecurityIncident
	if err := s.db.WithContext(ctx).Where("account_id = ? AND server_id = ? AND kind = ? AND state <> ?", srv.AccountID, srv.ID, "new_key", "resolved").Find(&rows).Error; err != nil {
		return
	}
	for _, row := range rows {
		if open[row.Subject] || !keyDriftSubject(row.Subject) {
			continue
		}
		s.resolveIncident(ctx, srv.AccountID, srv.ID, "new_key", row.Subject)
		s.notifyResolved(ctx, srv.AccountID, srv.ID, "new_key", row.Subject, "a fresh check no longer saw it")
	}
}

func keyDriftSubject(subject string) bool {
	return strings.HasPrefix(subject, "removed ") || strings.HasSuffix(subject, " changed") || strings.Contains(subject, " on ")
}

// BaselineVersionView is one approval of a server's keys.
type BaselineVersionView struct {
	Version    int
	Keys       int
	Reason     string
	ApprovedAt time.Time
	ApprovedBy string
}

// ApproveKeyBaseline stores the keys currently recorded for the server as
// a new baseline version, with an optional reason. It is refused when the
// recorded keys are not current: an old helper, a stale report, or key
// files the helper could not read. Unknown is never approved as expected.
// It does not change the server.
func (s *Service) ApproveKeyBaseline(ctx context.Context, a Actor, serverID int64, reason string) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) > 200 {
		return invalid("reason", "must be at most 200 characters")
	}
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return err
	}
	view, err := s.GetServer(ctx, a, serverID)
	if err != nil {
		return err
	}
	if why := keyCoverage(view, s.now()); why != "" {
		return invalid("server", "the recorded keys are not current (%s), so they cannot be approved", why)
	}
	var rows []store.KeyOccurrence
	if err := s.db.WithContext(ctx).Where("server_id = ?", srv.ID).Find(&rows).Error; err != nil {
		return err
	}
	items := make([]baselineKey, 0, len(rows))
	for _, r := range rows {
		items = append(items, baselineKey{User: r.User, Fingerprint: r.Fingerprint, From: r.From, HasCommand: r.HasCommand})
	}
	now := s.now()
	err = s.db.WithContext(ctx).Transaction(func(tx *gormDB) error {
		var last int
		if err := tx.Model(&store.KeyBaselineVersion{}).Where("server_id = ?", srv.ID).Select("COALESCE(MAX(version), 0)").Scan(&last).Error; err != nil {
			return err
		}
		ver := store.KeyBaselineVersion{ServerID: srv.ID, AccountID: srv.AccountID, Version: last + 1, Items: store.MustJSON(items), Reason: reason, ApprovedAt: now, ApprovedBy: a.UserID}
		if err := tx.Omit("Server").Create(&ver).Error; err != nil {
			return err
		}
		row := store.KeyBaseline{ServerID: srv.ID, AccountID: srv.AccountID, Items: store.MustJSON(items), ApprovedAt: now, ApprovedBy: a.UserID}
		return tx.Omit("Server").Save(&row).Error
	})
	if err != nil {
		return err
	}
	s.audit(ctx, nil, a, "key.baseline", srv.Name, map[string]any{"keys": len(items), "reason": reason})
	return nil
}

// BaselineVersions lists a server's key approvals, newest first.
func (s *Service) BaselineVersions(ctx context.Context, a Actor, serverID int64) ([]BaselineVersionView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	srv, _, err := s.loadVisibleServer(ctx, a, serverID)
	if err != nil {
		return nil, err
	}
	var rows []store.KeyBaselineVersion
	if err := s.db.WithContext(ctx).Where("server_id = ?", srv.ID).Order("version desc").Limit(50).Find(&rows).Error; err != nil {
		return nil, err
	}
	names := map[int64]string{}
	if srv.AccountID == a.AccountID {
		ids := make([]int64, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.ApprovedBy)
		}
		if names, err = s.userNames(ctx, ids); err != nil {
			return nil, err
		}
	}
	out := make([]BaselineVersionView, 0, len(rows))
	for _, r := range rows {
		var items []baselineKey
		_ = r.Items.Decode(&items)
		by := names[r.ApprovedBy]
		if by == "" {
			by = "a member of the owning space"
		}
		out = append(out, BaselineVersionView{Version: r.Version, Keys: len(items), Reason: r.Reason, ApprovedAt: r.ApprovedAt, ApprovedBy: by})
	}
	return out, nil
}

// ListKeys returns fingerprints seen on servers the actor can see.
// A fingerprint with no row was not in a standard authorized_keys file.
func (s *Service) ListKeys(ctx context.Context, a Actor) ([]KeyView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	views, err := s.serverViews(ctx, a, nil)
	if err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return []KeyView{}, nil
	}
	ids := make([]int64, len(views))
	names := map[int64]string{}
	for i, v := range views {
		ids[i] = v.ID
		names[v.ID] = v.Name
	}
	var rows []store.KeyOccurrence
	if err := s.db.WithContext(ctx).Where("server_id IN ?", ids).Order("fingerprint, server_id, user").Limit(keyListLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	var labels []store.KeyLabel
	if err := s.db.WithContext(ctx).Where("account_id = ?", a.AccountID).Find(&labels).Error; err != nil {
		return nil, err
	}
	labelOf := map[string]string{}
	for _, l := range labels {
		labelOf[l.Fingerprint] = l.Label
	}
	byFP := map[string]*KeyView{}
	var order []string
	for _, r := range rows {
		v, ok := byFP[r.Fingerprint]
		if !ok {
			v = &KeyView{Fingerprint: r.Fingerprint, KeyType: r.KeyType, HasCommand: r.HasCommand, Label: labelOf[r.Fingerprint], Uses: []KeyOccurrenceView{}}
			byFP[r.Fingerprint] = v
			order = append(order, r.Fingerprint)
		}
		if r.HasCommand {
			v.HasCommand = true
		}
		v.Uses = append(v.Uses, KeyOccurrenceView{
			ServerID: r.ServerID, ServerName: names[r.ServerID], User: r.User,
			Comment: r.Comment, From: r.From, HasCommand: r.HasCommand, ObservedAt: r.ObservedAt,
		})
	}
	out := make([]KeyView, 0, len(order))
	for _, fp := range order {
		out = append(out, *byFP[fp])
	}
	return out, nil
}

// LabelKey sets or clears the name for a fingerprint in the actor's space.
func (s *Service) LabelKey(ctx context.Context, a Actor, fingerprint, label string) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if !fingerprintRe.MatchString(fingerprint) {
		return invalid("fingerprint", "must be a SHA256 fingerprint")
	}
	label = strings.Join(strings.Fields(label), " ")
	if utf8.RuneCountInString(label) > 80 {
		return invalid("label", "must be at most 80 characters")
	}
	now := s.now()
	if label == "" {
		if err := s.db.WithContext(ctx).Where("account_id = ? AND fingerprint = ?", a.AccountID, fingerprint).Delete(&store.KeyLabel{}).Error; err != nil {
			return err
		}
	} else {
		row := store.KeyLabel{AccountID: a.AccountID, Fingerprint: fingerprint, Label: label, UpdatedAt: now}
		if err := s.db.WithContext(ctx).Save(&row).Error; err != nil {
			return err
		}
	}
	s.audit(ctx, nil, a, "key.label", fingerprint, map[string]any{"label": label})
	return nil
}

// keyListLimit caps the key catalog response, as the contract declares.
const keyListLimit = 2000
