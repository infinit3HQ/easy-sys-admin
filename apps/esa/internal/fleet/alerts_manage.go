package fleet

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// AlertView is an alert as the front doors show it.
type AlertView struct {
	ID           int64
	Kind         string
	ServerID     *int64
	ServerName   string
	Subject      string
	Severity     string
	Message      string
	PendingSince time.Time
	FiredAt      *time.Time
	ResolvedAt   *time.Time
}

// ListAlerts returns open alerts, or the newest alerts when includeResolved
// is set: the account's own, plus alerts on servers shared with it.
func (s *Service) ListAlerts(ctx context.Context, a Actor, includeResolved bool, limit int) ([]AlertView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := s.db.WithContext(ctx).Where("(alerts.account_id = ? OR alerts.server_id IN ("+sharedServerIDsSQL+"))", a.AccountID, a.AccountID).
		Preload("Server").Order("COALESCE(fired_at, pending_since) DESC").Limit(limit)
	if !includeResolved {
		q = q.Where("resolved_at IS NULL")
	}
	var rows []store.Alert
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AlertView, 0, len(rows))
	for _, r := range rows {
		v := AlertView{ID: r.ID, Kind: r.Kind, ServerID: r.ServerID, Subject: r.Subject, Severity: r.Severity,
			Message: r.Message, PendingSince: r.PendingSince, FiredAt: r.FiredAt, ResolvedAt: r.ResolvedAt}
		if r.Server != nil {
			v.ServerName = r.Server.Name
		}
		out = append(out, v)
	}
	return out, nil
}

// AlertRuleView is one rule.
type AlertRuleView struct {
	ID         int64
	Kind       string
	Enabled    bool
	Threshold  float64
	ForMinutes int
}

// ListAlertRules returns the account's rules, creating defaults on first use.
func (s *Service) ListAlertRules(ctx context.Context, a Actor) ([]AlertRuleView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	rules, err := s.rules(ctx, a.AccountID)
	if err != nil {
		return nil, err
	}
	out := []AlertRuleView{}
	for _, d := range defaultRules {
		if r, ok := rules[AlertKind(d.Kind)]; ok {
			out = append(out, AlertRuleView{ID: r.ID, Kind: r.Kind, Enabled: r.Enabled, Threshold: r.Threshold, ForMinutes: r.ForMinutes})
		}
	}
	return out, nil
}

// UpdateAlertRule changes one rule.
func (s *Service) UpdateAlertRule(ctx context.Context, a Actor, kind string, enabled bool, threshold float64, forMinutes int) (AlertRuleView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return AlertRuleView{}, err
	}
	rules, err := s.rules(ctx, a.AccountID)
	if err != nil {
		return AlertRuleView{}, err
	}
	r, ok := rules[AlertKind(kind)]
	if !ok {
		return AlertRuleView{}, ErrNotFound
	}
	switch AlertKind(kind) {
	case AlertDisk, AlertInode, AlertMemory:
		if threshold < 50 || threshold > 99 {
			return AlertRuleView{}, invalid("threshold", "must be between 50 and 99 percent")
		}
	case AlertCertExpiry:
		if threshold < 1 || threshold > 90 {
			return AlertRuleView{}, invalid("threshold", "must be between 1 and 90 days")
		}
	case AlertSecurityUpdate:
		if threshold < 0 || threshold > 90 {
			return AlertRuleView{}, invalid("threshold", "must be between 0 and 90 days")
		}
	default:
		threshold = r.Threshold
	}
	if forMinutes < 0 || forMinutes > 30*24*60 {
		return AlertRuleView{}, invalid("forMinutes", "must be between 0 and 43200")
	}
	if err := s.db.WithContext(ctx).Model(&store.AlertRule{}).Where("id = ?", r.ID).Updates(map[string]any{
		"enabled": enabled, "threshold": threshold, "for_minutes": forMinutes, "updated_at": s.now(),
	}).Error; err != nil {
		return AlertRuleView{}, err
	}
	s.audit(ctx, nil, a, "alert_rule.update", kind, map[string]any{"enabled": enabled, "threshold": threshold, "for_minutes": forMinutes})
	return AlertRuleView{ID: r.ID, Kind: kind, Enabled: enabled, Threshold: threshold, ForMinutes: forMinutes}, nil
}

// ---- Notification channels -------------------------------------------------------

// ChannelView is a channel without its secret configuration.
type ChannelView struct {
	ID         int64
	Name       string
	Kind       string
	Enabled    bool
	Target     string // a safe description, for example the ntfy host and topic
	LastError  string
	LastSentAt *time.Time
	CreatedAt  time.Time
}

// ChannelConfig is the decrypted configuration of a channel.
type ChannelConfig struct {
	URL      string   `json:"url,omitempty"`
	Token    string   `json:"token,omitempty"`  // ntfy access token
	Secret   string   `json:"secret,omitempty"` // webhook HMAC secret
	SMTPHost string   `json:"smtpHost,omitempty"`
	SMTPPort int      `json:"smtpPort,omitempty"`
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`
	From     string   `json:"from,omitempty"`
	To       []string `json:"to,omitempty"`
}

// ChannelValidator checks a channel configuration. internal/notify implements it.
type ChannelValidator interface {
	Validate(kind string, cfg ChannelConfig) (target string, err error)
}

func channelContext(id int64) string { return "channel:" + strconv.FormatInt(id, 10) }

// ListChannels returns the account's channels.
func (s *Service) ListChannels(ctx context.Context, a Actor) ([]ChannelView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	var rows []store.NotifyChannel
	if err := s.scoped(ctx, a).Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ChannelView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.channelView(r))
	}
	return out, nil
}

func (s *Service) channelView(r store.NotifyChannel) ChannelView {
	v := ChannelView{ID: r.ID, Name: r.Name, Kind: r.Kind, Enabled: r.Enabled, LastError: r.LastError, LastSentAt: r.LastSentAt, CreatedAt: r.CreatedAt}
	if raw, err := s.box.Open(r.ConfigEnc, channelContext(r.ID)); err == nil {
		var cfg ChannelConfig
		if json.Unmarshal(raw, &cfg) == nil {
			if v2, ok := s.notifier.(ChannelValidator); ok {
				v.Target, _ = v2.Validate(r.Kind, cfg)
			}
		}
	}
	return v
}

// CreateChannel adds a notification channel. Owner only, because the
// configuration holds credentials.
func (s *Service) CreateChannel(ctx context.Context, a Actor, name, kind string, cfg ChannelConfig) (ChannelView, error) {
	if err := a.can(RoleOwner, ScopeAdmin); err != nil {
		return ChannelView{}, err
	}
	name, err := validName("name", name)
	if err != nil {
		return ChannelView{}, err
	}
	if err := s.underCap(ctx, a, &store.NotifyChannel{}, maxChannels, "notification channels"); err != nil {
		return ChannelView{}, err
	}
	if v, ok := s.notifier.(ChannelValidator); ok {
		if _, err := v.Validate(kind, cfg); err != nil {
			return ChannelView{}, invalid("config", "%s", err.Error())
		}
	}
	raw, _ := json.Marshal(cfg)
	ch := store.NotifyChannel{AccountID: a.AccountID, Name: name, Kind: kind, Enabled: true, CreatedAt: s.now(), ConfigEnc: "pending"}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		tx := q.Raw()
		if err := tx.Omit(clause.Associations).Create(&ch).Error; err != nil {
			return mapStore(err)
		}
		sealed, err := s.box.Seal(raw, channelContext(ch.ID))
		if err != nil {
			return err
		}
		ch.ConfigEnc = sealed
		if err := tx.Model(&ch).Update("config_enc", sealed).Error; err != nil {
			return err
		}
		s.audit(ctx, q, a, "channel.create", name, map[string]any{"kind": kind})
		return nil
	})
	if err != nil {
		return ChannelView{}, err
	}
	return s.channelView(ch), nil
}

// SetChannelEnabled turns a channel on or off.
func (s *Service) SetChannelEnabled(ctx context.Context, a Actor, id int64, enabled bool) error {
	if err := a.can(RoleOwner, ScopeAdmin); err != nil {
		return err
	}
	res := s.scoped(ctx, a).Model(&store.NotifyChannel{}).Where("id = ?", id).Update("enabled", enabled)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	s.audit(ctx, nil, a, "channel.update", strconv.FormatInt(id, 10), map[string]any{"enabled": enabled})
	return nil
}

// DeleteChannel removes a channel.
func (s *Service) DeleteChannel(ctx context.Context, a Actor, id int64) error {
	if err := a.can(RoleOwner, ScopeAdmin); err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Where("id = ? AND account_id = ?", id, a.AccountID).Delete(&store.NotifyChannel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	s.audit(ctx, nil, a, "channel.delete", strconv.FormatInt(id, 10), nil)
	return nil
}

// TestChannel sends a test message through one channel and returns the
// error, if any. Unlike alerts it is sent right away, not queued.
func (s *Service) TestChannel(ctx context.Context, a Actor, id int64) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	if err := s.limitRate(a, "channel.test"); err != nil {
		return err
	}
	var ch store.NotifyChannel
	if err := s.scoped(ctx, a).Where("id = ?", id).Take(&ch).Error; err != nil {
		return mapStore(err)
	}
	if s.notifier == nil {
		return &UnavailableError{Message: "notifications are not configured"}
	}
	err := s.sendOne(ctx, ch, Message{Title: "esa test notification", Body: "This channel works. Alerts from easy-sys-admin will arrive here.", Severity: "info", URL: s.publicURL.JoinPath("/app/alerts").String()})
	s.audit(ctx, nil, a, "channel.test", ch.Name, map[string]any{"ok": err == nil})
	if err != nil {
		return &UnavailableError{Message: netErrorText(err)}
	}
	return nil
}

// notify queues a message for every enabled channel of the account.
// Delivery happens in the background (see enqueueNotify).
func (s *Service) notify(ctx context.Context, accountID int64, msg Message) {
	if s.notifier == nil {
		return
	}
	var chans []store.NotifyChannel
	if err := s.db.WithContext(ctx).Where("account_id = ? AND enabled", accountID).Find(&chans).Error; err != nil {
		return
	}
	if s.silenced(ctx, accountID, msg.ServerID, msg.Kind) {
		return
	}
	if len(chans) > 0 && s.holdForSummary(ctx, accountID, msg) {
		return
	}
	for _, ch := range chans {
		s.enqueueNotify(ch, msg)
	}
}

func (s *Service) silenced(ctx context.Context, accountID, serverID int64, kind string) bool {
	if kind == string(AlertHostKey) || kind == string(AlertNewKey) {
		return false
	}
	now := s.now()
	var n int64
	q := s.db.WithContext(ctx).Model(&store.MaintenanceWindow{}).
		Where("account_id = ? AND starts_at <= ? AND ends_at > ?", accountID, now, now)
	if serverID == 0 {
		q = q.Where("server_id IS NULL AND group_id IS NULL")
	} else {
		q = q.Where("server_id = ? OR (server_id IS NULL AND (group_id IS NULL OR group_id = (SELECT group_id FROM servers WHERE id = ?)))", serverID, serverID)
	}
	q.Count(&n)
	return n > 0
}

func (s *Service) sendOne(ctx context.Context, ch store.NotifyChannel, msg Message) error {
	raw, err := s.box.Open(ch.ConfigEnc, channelContext(ch.ID))
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	err = s.notifier.Send(cctx, ch, raw, msg)
	now := s.now()
	changes := map[string]any{"last_error": ""}
	if err != nil {
		// The channel page shows last_error to every member: store the
		// category only, and log the detail.
		s.log.Warn("send notification", "account_id", ch.AccountID, "channel", ch.Name, "err", err)
		changes["last_error"] = netErrorText(err)
	} else {
		changes["last_sent_at"] = now
	}
	s.db.WithContext(context.WithoutCancel(ctx)).Model(&store.NotifyChannel{}).Where("id = ?", ch.ID).Updates(changes)
	return err
}
