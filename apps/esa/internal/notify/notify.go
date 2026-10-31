// Package notify delivers alert messages to ntfy, webhooks (generic,
// Discord, Slack), and email.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/netguard"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/version"
)

// Sender implements fleet.Notifier and fleet.ChannelValidator.
type Sender struct {
	http   *http.Client
	dialer *net.Dialer
}

// New returns a Sender whose connections go through the address policy:
// metadata, link-local, and unspecified addresses are always refused, and
// loopback and private addresses unless allowPrivate is set. The check runs
// on the resolved address at dial time, so DNS rebinding cannot bypass it.
func New(allowPrivate bool) *Sender {
	dialer := netguard.Dialer(allowPrivate, net.Dialer{Timeout: 10 * time.Second})
	return &Sender{dialer: dialer, http: &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

var (
	_ fleet.Notifier         = (*Sender)(nil)
	_ fleet.ChannelValidator = (*Sender)(nil)
)

// Validate checks a configuration and returns a description safe to show.
func (s *Sender) Validate(kind string, c fleet.ChannelConfig) (string, error) {
	switch kind {
	case "ntfy", "webhook", "discord", "slack":
		u, err := url.Parse(c.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return "", errors.New("url must be an http or https URL")
		}
		if kind == "ntfy" && strings.Trim(u.Path, "/") == "" {
			return "", errors.New("the ntfy URL must include the topic, for example https://ntfy.sh/my-servers")
		}
		target := u.Host
		if kind == "ntfy" {
			target += u.Path // the topic is not secret on its own when a token is set; shown so people recognise it
		}
		return kind + " " + target, nil
	case "email":
		if c.SMTPHost == "" || c.SMTPPort <= 0 || c.SMTPPort > 65535 {
			return "", errors.New("smtpHost and smtpPort are required")
		}
		if _, err := mail.ParseAddress(c.From); err != nil {
			return "", errors.New("from must be an email address")
		}
		if len(c.To) == 0 {
			return "", errors.New("add at least one recipient")
		}
		for _, to := range c.To {
			if _, err := mail.ParseAddress(to); err != nil {
				return "", fmt.Errorf("%q is not an email address", to)
			}
		}
		return "email to " + strings.Join(c.To, ", "), nil
	}
	return "", errors.New("kind must be ntfy, webhook, discord, slack, or email")
}

// Send delivers one message.
func (s *Sender) Send(ctx context.Context, ch store.NotifyChannel, raw []byte, m fleet.Message) error {
	var c fleet.ChannelConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return err
	}
	title := m.Title
	if len(title) > 250 {
		title = title[:250]
	}
	switch ch.Kind {
	case "ntfy":
		return s.ntfy(ctx, c, title, m)
	case "discord":
		return s.postJSON(ctx, c.URL, map[string]any{"content": emoji(m) + " **" + title + "**\n" + m.URL}, nil)
	case "slack":
		return s.postJSON(ctx, c.URL, map[string]any{"text": emoji(m) + " *" + title + "*\n" + m.URL}, nil)
	case "webhook":
		body := map[string]any{
			"title": title, "body": m.Body, "severity": m.Severity, "resolved": m.Resolved,
			"url": m.URL, "sentAt": time.Now().UTC(), "source": "easy-sys-admin",
		}
		return s.postJSON(ctx, c.URL, body, []byte(c.Secret))
	case "email":
		return s.sendMail(ctx, c, title, m)
	}
	return fmt.Errorf("unknown channel kind %q", ch.Kind)
}

func emoji(m fleet.Message) string {
	switch {
	case m.Resolved:
		return "✅"
	case m.Severity == "critical":
		return "🔴"
	case m.Severity == "warning":
		return "🟠"
	}
	return "ℹ️"
}

func (s *Sender) ntfy(ctx context.Context, c fleet.ChannelConfig, title string, m fleet.Message) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, strings.NewReader(m.Body))
	if err != nil {
		return err
	}
	req.Header.Set("Title", title)
	prio := map[string]string{"critical": "urgent", "warning": "high", "info": "default"}[m.Severity]
	if m.Resolved {
		prio = "low"
	}
	if prio != "" {
		req.Header.Set("Priority", prio)
	}
	if m.URL != "" {
		req.Header.Set("Click", m.URL)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return s.do(req)
}

func (s *Sender) postJSON(ctx context.Context, target string, body any, secret []byte) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "easy-sys-admin/"+version.Version)
	if len(secret) > 0 {
		mac := hmac.New(sha256.New, secret)
		mac.Write(raw)
		req.Header.Set("X-Esa-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	return s.do(req)
}

func (s *Sender) do(req *http.Request) error {
	resp, err := s.http.Do(req)
	if err != nil {
		// The URL can hold a webhook secret; report only the host.
		return fmt.Errorf("send to %s: %w", req.URL.Host, unwrapURLError(err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("send to %s: %w", req.URL.Host, &fleet.HTTPStatusError{Code: resp.StatusCode})
	}
	return nil
}

func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func (s *Sender) sendMail(ctx context.Context, c fleet.ChannelConfig, title string, m fleet.Message) error {
	from, _ := mail.ParseAddress(c.From)
	var to []string
	for _, t := range c.To {
		a, _ := mail.ParseAddress(t)
		to = append(to, a.Address)
	}
	subject := strings.NewReplacer("\r", " ", "\n", " ").Replace("[esa] " + title)
	var body strings.Builder
	fmt.Fprintf(&body, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nDate: %s\r\n\r\n",
		from.String(), strings.Join(to, ", "), subject, time.Now().UTC().Format(time.RFC1123Z))
	body.WriteString(m.Body)
	if m.URL != "" {
		body.WriteString("\r\n\r\n" + m.URL)
	}
	body.WriteString("\r\n")

	var auth smtp.Auth
	if c.Username != "" {
		auth = smtp.PlainAuth("", c.Username, c.Password, c.SMTPHost)
	}
	return s.smtpSend(ctx, c.SMTPHost, c.SMTPPort, auth, from.Address, to, []byte(body.String()))
}

// smtpSend is smtp.SendMail with a context: it dials through the address
// policy and bounds the whole conversation by ctx.
func (s *Sender) smtpSend(ctx context.Context, host string, port int, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := s.dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("send to %s: %w", host, err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Hello("localhost"); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("smtp: server doesn't support AUTH")
		}
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, addr := range to {
		if err := c.Rcpt(addr); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
