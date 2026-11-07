// Package authmail sends confirmation and password-reset messages through
// the install's own SMTP account. Alert channels keep their own settings.
package authmail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/config"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/netguard"
)

// Sender delivers one plain-text message. Connections go through the same
// address policy as alert mail, and cleartext is refused.
type Sender struct {
	cfg    config.SMTPConfig
	dialer *net.Dialer
}

// New returns a sender. allowPrivate matches the rest of the control plane,
// so a mail server on a private address needs ESA_ALLOW_PRIVATE_NETWORKS.
func New(cfg config.SMTPConfig, allowPrivate bool) *Sender {
	return &Sender{
		cfg:    cfg,
		dialer: netguard.Dialer(allowPrivate, net.Dialer{Timeout: 10 * time.Second}),
	}
}

// Send delivers one message. The context bounds the SMTP conversation.
func (s *Sender) Send(ctx context.Context, to, subject, body string) error {
	from, err := mail.ParseAddress(s.cfg.From)
	if err != nil {
		return err
	}
	toAddr, err := mail.ParseAddress(to)
	if err != nil {
		return err
	}
	subject = strings.NewReplacer("\r", " ", "\n", " ").Replace(subject)
	var msg strings.Builder
	fmt.Fprintf(&msg, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nDate: %s\r\n\r\n%s",
		from.String(), toAddr.Address, subject, time.Now().UTC().Format(time.RFC1123Z), body)
	if !strings.HasSuffix(body, "\n") {
		msg.WriteString("\r\n")
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	conn, err := s.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Hello("localhost"); err != nil {
		return err
	}
	if s.cfg.Port != 465 {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp: server does not offer STARTTLS")
		}
		if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host}); err != nil {
			return err
		}
	}
	if s.cfg.Username != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("smtp: server does not offer AUTH")
		}
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(toAddr.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg.String())); err != nil {
		return err
	}
	return w.Close()
}

func (s *Sender) dial(ctx context.Context) (net.Conn, error) {
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	if s.cfg.Port == 465 {
		raw, err := s.dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		tlsConn := tls.Client(raw, &tls.Config{ServerName: s.cfg.Host})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return nil, err
		}
		return tlsConn, nil
	}
	return s.dialer.DialContext(ctx, "tcp", addr)
}
