package fleet

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

type KeyExceptionView struct {
	ID          int64
	ServerID    int64
	User        string
	Fingerprint string
	Until       time.Time
	Reason      string
}

func (s *Service) ListKeyExceptions(ctx context.Context, a Actor, serverID int64) ([]KeyExceptionView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	srv, _, err := s.loadVisibleServer(ctx, a, serverID)
	if err != nil {
		return nil, err
	}
	var rows []store.KeyException
	if err := s.db.WithContext(ctx).Where("server_id = ?", srv.ID).Order("until desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]KeyExceptionView, 0, len(rows))
	for _, r := range rows {
		out = append(out, KeyExceptionView{ID: r.ID, ServerID: r.ServerID, User: r.User, Fingerprint: r.Fingerprint, Until: r.Until, Reason: r.Reason})
	}
	return out, nil
}

func (s *Service) CreateKeyException(ctx context.Context, a Actor, serverID int64, user, fingerprint, reason string, until time.Time) (KeyExceptionView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return KeyExceptionView{}, err
	}
	user = strings.TrimSpace(user)
	fingerprint = strings.TrimSpace(fingerprint)
	reason = strings.TrimSpace(reason)
	if user == "" || utf8.RuneCountInString(user) > 64 {
		return KeyExceptionView{}, invalid("user", "use 1 to 64 characters")
	}
	if !fingerprintRe.MatchString(fingerprint) {
		return KeyExceptionView{}, invalid("fingerprint", "must be a SHA256 fingerprint")
	}
	if reason == "" || utf8.RuneCountInString(reason) > 200 {
		return KeyExceptionView{}, invalid("reason", "say why, in at most 200 characters")
	}
	now := s.now()
	if !until.After(now) || until.After(now.Add(90*24*time.Hour)) {
		return KeyExceptionView{}, invalid("until", "must be within the next 90 days")
	}
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return KeyExceptionView{}, err
	}
	var row store.KeyException
	err = s.db.WithContext(ctx).Where("server_id = ? AND user = ? AND fingerprint = ?", srv.ID, user, fingerprint).Take(&row).Error
	if err == nil {
		row.Until, row.Reason = until, reason
		if err := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{"until": until, "reason": reason}).Error; err != nil {
			return KeyExceptionView{}, err
		}
	} else {
		row = store.KeyException{AccountID: srv.AccountID, ServerID: srv.ID, User: user, Fingerprint: fingerprint, Until: until, Reason: reason, CreatedAt: now}
		if err := s.db.WithContext(ctx).Omit("Account", "Server").Create(&row).Error; err != nil {
			return KeyExceptionView{}, err
		}
	}
	s.audit(ctx, nil, a, "key.exception", srv.Name, map[string]any{"user": user, "until": until, "reason": reason})
	return KeyExceptionView{ID: row.ID, ServerID: row.ServerID, User: row.User, Fingerprint: row.Fingerprint, Until: row.Until, Reason: row.Reason}, nil
}

func (s *Service) DeleteKeyException(ctx context.Context, a Actor, serverID, id int64) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Where("id = ? AND server_id = ?", id, srv.ID).Delete(&store.KeyException{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	s.audit(ctx, nil, a, "key.exception.delete", srv.Name, map[string]any{"id": id})
	return nil
}

func (s *Service) keyExceptions(ctx context.Context, serverID int64) map[string]bool {
	var rows []store.KeyException
	if err := s.db.WithContext(ctx).Where("server_id = ? AND until > ?", serverID, s.now()).Find(&rows).Error; err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, r := range rows {
		out[r.User+"\n"+r.Fingerprint] = true
	}
	return out
}
