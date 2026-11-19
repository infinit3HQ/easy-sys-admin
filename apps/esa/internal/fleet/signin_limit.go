package fleet

import (
	"context"
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

// signInGate bounds how many sign-in attempts in this process wait on the
// database at once, so a flood of attempts cannot take every pooled
// connection away from the rest of the API.
var signInGate = make(chan struct{}, 4)

// SignInTicket is one password attempt that has been counted. It is counted
// before the password is checked, so parallel guesses cannot all pass a
// limit that only the failures would have reached.
type SignInTicket struct {
	ids       []int64
	email, ip string
}

// SignInReserve counts this attempt and reports why it must stop, or "" when
// it may go ahead. Attempts at one address are counted and checked under a
// lock on that address, so a burst of parallel guesses at it is held to the
// same limits as a slow one. The per-client and install-wide counts are not
// locked across addresses and can run slightly over in a burst. A database
// error stops the attempt, so a second process cannot guess while the shared
// count is unreadable. Call SignInRelease or SignInSucceed when the attempt
// turns out not to be a wrong guess.
func (s *Service) SignInReserve(ctx context.Context, email, ip string) (SignInTicket, string) {
	email, ip = signInKeyPart(email), signInKeyPart(ip)
	ticket := SignInTicket{email: email, ip: ip}
	wait := time.NewTimer(2 * time.Second)
	defer wait.Stop()
	select {
	case signInGate <- struct{}{}:
		defer func() { <-signInGate }()
	case <-wait.C:
		return SignInTicket{}, "rate_limited"
	case <-ctx.Done():
		return SignInTicket{}, "rate_limited"
	}
	reason := ""
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// A queue behind the lock must not hold a connection for long.
		if err := tx.Exec("SET LOCAL lock_timeout = '2s'").Error; err != nil {
			return err
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "esa-signin:"+email).Error; err != nil {
			return err
		}
		now := s.now()
		n, err := signInCounts(tx, now, email, ip)
		if err != nil {
			return err
		}
		switch {
		case n.pair >= 5 || n.email >= 100:
			reason = "locked"
			return nil
		case n.client >= 30 || n.global >= 1000:
			reason = "rate_limited"
			return nil
		}
		if err := tx.Where("at < ?", now.Add(-authKeep)).Delete(&store.SignInHit{}).Error; err != nil {
			return err
		}
		keys := []string{signInPair(email, ip), "email\n" + email, "client\n" + ip, "global"}
		rows := make([]store.SignInHit, len(keys))
		for i, key := range keys {
			rows[i] = store.SignInHit{Key: key, At: now}
		}
		if err := tx.Create(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			ticket.ids = append(ticket.ids, r.ID)
		}
		return nil
	})
	if err != nil {
		s.log.Error("sign-in limit", "err", err)
		return SignInTicket{}, "rate_limited"
	}
	return ticket, reason
}

// SignInRelease takes back a counted attempt that was not a wrong guess, such
// as a correct password on an account that still has to confirm its address.
func (s *Service) SignInRelease(t SignInTicket) {
	if len(t.ids) == 0 {
		return
	}
	if err := s.db.Where("id IN ?", t.ids).Delete(&store.SignInHit{}).Error; err != nil {
		s.log.Error("release sign-in attempt", "err", err)
	}
}

// SignInSucceed releases the attempt and clears the counters only the account
// owner can clear. The client and whole-install budgets keep their earlier
// failures, so one sign-in does not buy more guesses at other accounts.
func (s *Service) SignInSucceed(t SignInTicket) {
	s.SignInRelease(t)
	err := s.db.Where("key IN ?", []string{signInPair(t.email, t.ip), "email\n" + t.email}).Delete(&store.SignInHit{}).Error
	if err != nil {
		s.log.Error("clear sign-in limit", "err", err)
	}
}

type signInCount struct{ pair, email, client, global int64 }

func signInCounts(db *gorm.DB, now time.Time, email, ip string) (signInCount, error) {
	since := now.Add(-signInWindow)
	var rows []struct {
		Key string
		N   int64
	}
	err := db.Model(&store.SignInHit{}).
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
