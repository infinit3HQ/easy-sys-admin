package fleet

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const signInWindow = 15 * time.Minute

// authKeep is the longest shared auth window. Older rows are deleted.
// Each check still counts only its own window.
const authKeep = time.Hour

// AuthLimited reports whether key has already been used max times inside window.
// A database error counts as limited, so a second process cannot spend a quota
// the shared count cannot show.
func (s *Service) AuthLimited(key string, max int, window time.Duration) bool {
	n, err := s.countHit(signInKeyPart(key), window)
	if err != nil {
		s.log.Error("auth limit", "err", err)
		return true
	}
	return n >= int64(max)
}

// AuthHit records one use of key for the shared auth quotas.
func (s *Service) AuthHit(key string) {
	if err := s.recordHits([]string{signInKeyPart(key)}); err != nil {
		s.log.Error("record auth limit", "err", err)
	}
}

// SignInBlocked reports why this password attempt must stop, or "" when it
// may go ahead. A database error stops the attempt, so a second process
// cannot guess while the shared count is unreadable.
func (s *Service) SignInBlocked(email, ip string) string {
	email, ip = signInKeyPart(email), signInKeyPart(ip)
	n, err := s.signInCounts(email, ip)
	if err != nil {
		s.log.Error("sign-in limit", "err", err)
		return "rate_limited"
	}
	if n.pair >= 5 || n.email >= 100 {
		return "locked"
	}
	if n.client >= 30 || n.global >= 1000 {
		return "rate_limited"
	}
	return ""
}

// SignInFail records one failed password sign-in on the shared rows.
func (s *Service) SignInFail(email, ip string) {
	email, ip = signInKeyPart(email), signInKeyPart(ip)
	keys := []string{signInPair(email, ip), "email\n" + email, "client\n" + ip, "global"}
	if err := s.recordHits(keys); err != nil {
		s.log.Error("record sign-in failure", "err", err)
	}
}

// SignInSucceed clears the counters only the account owner can clear.
// The client and whole-install budgets stay, so one sign-in does not buy
// more guesses at other accounts.
func (s *Service) SignInSucceed(email, ip string) {
	email, ip = signInKeyPart(email), signInKeyPart(ip)
	err := s.db.Where("key IN ?", []string{signInPair(email, ip), "email\n" + email}).Delete(&store.SignInHit{}).Error
	if err != nil {
		s.log.Error("clear sign-in limit", "err", err)
	}
}

type signInCount struct{ pair, email, client, global int64 }

func (s *Service) signInCounts(email, ip string) (signInCount, error) {
	since := s.now().Add(-signInWindow)
	var rows []struct {
		Key string
		N   int64
	}
	err := s.db.Model(&store.SignInHit{}).
		Select("key, count(*) as n").
		Where("at >= ? AND key IN ?", since, []string{signInPair(email, ip), "email\n" + email, "client\n" + ip, "global"}).
		Group("key").
		Scan(&rows).Error
	if err != nil {
		return signInCount{}, err
	}
	var c signInCount
	for _, r := range rows {
		switch {
		case strings.HasPrefix(r.Key, "pair\n"):
			c.pair = r.N
		case strings.HasPrefix(r.Key, "email\n"):
			c.email = r.N
		case strings.HasPrefix(r.Key, "client\n"):
			c.client = r.N
		case r.Key == "global":
			c.global = r.N
		}
	}
	return c, nil
}

func (s *Service) countHit(key string, window time.Duration) (int64, error) {
	var n int64
	err := s.db.Model(&store.SignInHit{}).Where("key = ? AND at >= ?", key, s.now().Add(-window)).Count(&n).Error
	return n, err
}

func (s *Service) recordHits(keys []string) error {
	now := s.now()
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("at < ?", now.Add(-authKeep)).Delete(&store.SignInHit{}).Error; err != nil {
			return err
		}
		rows := make([]store.SignInHit, len(keys))
		for i, key := range keys {
			rows[i] = store.SignInHit{Key: key, At: now}
		}
		return tx.Create(&rows).Error
	})
}

func signInPair(email, ip string) string { return "pair\n" + email + "\n" + ip }

func signInKeyPart(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
