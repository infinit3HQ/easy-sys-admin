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
func (s *Service) syncKeyOccurrences(ctx context.Context, srv store.Server, keys []keyUse, at time.Time) {
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
	}
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
	if err := s.db.WithContext(ctx).Where("server_id IN ?", ids).Order("fingerprint, server_id, user").Find(&rows).Error; err != nil {
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
			Comment: r.Comment, From: r.From, ObservedAt: r.ObservedAt,
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
