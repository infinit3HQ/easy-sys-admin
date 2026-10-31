package fleet

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/netguard"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// CertView is one checked certificate.
type CertView struct {
	ID        int64
	Hostname  string
	Port      int
	ExpiresAt *time.Time
	DaysLeft  *int
	Issuer    string
	LastError string
	CheckedAt *time.Time
}

func toCertView(c store.CertTarget, now time.Time) CertView {
	v := CertView{ID: c.ID, Hostname: c.Hostname, Port: c.Port, ExpiresAt: c.ExpiresAt, Issuer: c.Issuer, LastError: c.LastError, CheckedAt: c.CheckedAt}
	if c.ExpiresAt != nil {
		d := int(c.ExpiresAt.Sub(now).Hours() / 24)
		v.DaysLeft = &d
	}
	return v
}

// ListCerts returns the account's certificate targets, soonest expiry first.
func (s *Service) ListCerts(ctx context.Context, a Actor) ([]CertView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	var rows []store.CertTarget
	if err := s.scoped(ctx, a).Order("expires_at NULLS FIRST, hostname").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]CertView, 0, len(rows))
	for _, r := range rows {
		out = append(out, toCertView(r, s.now()))
	}
	return out, nil
}

// AddCert adds a hostname to check and checks it right away.
func (s *Service) AddCert(ctx context.Context, a Actor, hostname string, port int) (CertView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return CertView{}, err
	}
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if !hostnameRe.MatchString(hostname) || !strings.Contains(hostname, ".") {
		return CertView{}, invalid("hostname", "enter a hostname such as example.com")
	}
	if port == 0 {
		port = 443
	}
	if _, err := validPort(port); err != nil {
		return CertView{}, err
	}
	if err := s.underCap(ctx, a, &store.CertTarget{}, maxCerts, "certificates"); err != nil {
		return CertView{}, err
	}
	if err := s.limitRate(a, "cert.add"); err != nil {
		return CertView{}, err
	}
	c := store.CertTarget{AccountID: a.AccountID, Hostname: hostname, Port: port, CreatedAt: s.now()}
	if err := s.db.WithContext(ctx).Omit(clause.Associations).Create(&c).Error; err != nil {
		return CertView{}, mapStore(err)
	}
	s.audit(ctx, nil, a, "cert.add", hostname, nil)
	s.checkCert(ctx, &c)
	return toCertView(c, s.now()), nil
}

// DeleteCert removes a certificate target.
func (s *Service) DeleteCert(ctx context.Context, a Actor, id int64) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Where("id = ? AND account_id = ?", id, a.AccountID).Delete(&store.CertTarget{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	s.audit(ctx, nil, a, "cert.delete", strconv.FormatInt(id, 10), nil)
	return nil
}

// CheckCerts checks every certificate target and updates expiry alerts.
func (s *Service) CheckCerts(ctx context.Context) {
	var rows []store.CertTarget
	if err := s.db.WithContext(ctx).Order("account_id").Find(&rows).Error; err != nil {
		s.log.Error("load cert targets", "err", err)
		return
	}
	byAccount := map[int64][]store.CertTarget{}
	for i := range rows {
		s.checkCert(ctx, &rows[i])
		byAccount[rows[i].AccountID] = append(byAccount[rows[i].AccountID], rows[i])
	}
	for accountID, certs := range byAccount {
		rules, err := s.rules(ctx, accountID)
		if err != nil || !rules[AlertCertExpiry].Enabled {
			continue
		}
		var conds []condition
		for _, c := range certs {
			name := c.Hostname + ":" + strconv.Itoa(c.Port)
			switch {
			case c.LastError != "":
				conds = append(conds, condition{kind: AlertCertExpiry, subject: name, severity: "warning", message: "certificate check for " + name + " failed: " + c.LastError})
			case c.ExpiresAt != nil:
				days := c.ExpiresAt.Sub(s.now()).Hours() / 24
				if days <= rules[AlertCertExpiry].Threshold {
					sev := "warning"
					if days <= 3 {
						sev = "critical"
					}
					conds = append(conds, condition{kind: AlertCertExpiry, subject: name, severity: sev, message: fmt.Sprintf("certificate for %s expires in %.0f days", name, days)})
				}
			}
		}
		s.reconcile(ctx, accountID, nil, rules, conds, func(k AlertKind) bool { return k == AlertCertExpiry })
	}
}

func (s *Service) checkCert(ctx context.Context, c *store.CertTarget) {
	now := s.now()
	c.CheckedAt = &now
	// The hostname is user input: dial through the address policy.
	nd := netguard.Dialer(s.allowPrivate, net.Dialer{Timeout: 10 * time.Second})
	d := &tls.Dialer{NetDialer: nd, Config: &tls.Config{ServerName: c.Hostname}}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	conn, err := d.DialContext(cctx, "tcp", net.JoinHostPort(c.Hostname, strconv.Itoa(c.Port)))
	if err != nil {
		s.log.Info("certificate check failed", "account_id", c.AccountID, "host", c.Hostname, "port", c.Port, "err", err)
		c.LastError = netErrorText(err)
	} else {
		state := conn.(*tls.Conn).ConnectionState()
		_ = conn.Close()
		if len(state.PeerCertificates) > 0 {
			leaf := state.PeerCertificates[0]
			exp := leaf.NotAfter.UTC()
			c.ExpiresAt, c.Issuer, c.LastError = &exp, leaf.Issuer.CommonName, ""
			if len(leaf.Issuer.Organization) > 0 {
				c.Issuer = leaf.Issuer.Organization[0]
			}
		}
	}
	s.db.WithContext(ctx).Model(&store.CertTarget{}).Where("id = ?", c.ID).Updates(map[string]any{
		"expires_at": c.ExpiresAt, "issuer": c.Issuer, "last_error": c.LastError, "checked_at": now,
	})
}
