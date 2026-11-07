package fleet

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/password"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// AuthMailer sends one plain-text message. The control plane uses it for
// address confirmation and password reset.
type AuthMailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

const (
	confirmTTL = 24 * time.Hour
	resetTTL   = time.Hour
)

// RequestPasswordReset emails a reset link when the address belongs to a
// password account. Any other address is a success and sends nothing, so
// the caller cannot tell whether the account exists.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	if s.authMail == nil {
		return ErrMailFailed
	}
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	u, ok := s.localUser(ctx, email)
	if !ok {
		return nil
	}
	if err := s.sendAuthMail(ctx, u.ID, u.Email, "reset", ""); err != nil {
		return fmt.Errorf("%w: %w", ErrMailFailed, err)
	}
	s.audit(ctx, nil, Actor{UserID: u.ID, AccountID: 0, Source: SourceWeb}, "auth.reset_request", email, nil)
	return nil
}

// ResendConfirmation emails a new confirmation link for an unconfirmed
// password account. Any other address is a success and sends nothing.
func (s *Service) ResendConfirmation(ctx context.Context, email string) error {
	if s.authMail == nil {
		return ErrMailFailed
	}
	email, err := NormalizeEmail(email)
	if err != nil {
		return err
	}
	u, ok := s.localUser(ctx, email)
	if !ok || u.EmailConfirmed {
		return nil
	}
	if err := s.sendAuthMail(ctx, u.ID, u.Email, "confirm", ""); err != nil {
		return fmt.Errorf("%w: %w", ErrMailFailed, err)
	}
	return nil
}

// ConfirmEmail marks the password account behind a confirmation link as confirmed.
func (s *Service) ConfirmEmail(ctx context.Context, token string) error {
	u, pending, err := s.consumeAuthToken(ctx, token, "confirm")
	if err != nil {
		return err
	}
	updates := map[string]any{"email_confirmed": true}
	if pending != "" {
		updates["password_hash"] = pending
	}
	if err := s.db.WithContext(ctx).Model(&store.User{}).Where("id = ?", u.ID).Updates(updates).Error; err != nil {
		return err
	}
	s.audit(ctx, nil, Actor{UserID: u.ID, Source: SourceWeb}, "auth.confirm", u.Email, nil)
	return nil
}

// ResetPassword sets a new password from a reset link, confirms the address,
// and ends every session for that user.
func (s *Service) ResetPassword(ctx context.Context, token, pw string) error {
	if err := validPassword(pw, ""); err != nil {
		return err
	}
	u, _, err := s.consumeAuthToken(ctx, token, "reset")
	if err != nil {
		return err
	}
	if err := validPassword(pw, u.Email); err != nil {
		return err
	}
	hash, err := password.Hash(ctx, pw)
	if err != nil {
		return err
	}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if err := q.Raw().WithContext(ctx).Model(&store.User{}).Where("id = ?", u.ID).
			Updates(map[string]any{"password_hash": hash, "email_confirmed": true}).Error; err != nil {
			return err
		}
		return q.Raw().WithContext(ctx).Where("user_id = ?", u.ID).Delete(&store.Session{}).Error
	})
	if err != nil {
		return err
	}
	s.audit(ctx, nil, Actor{UserID: u.ID, Source: SourceWeb}, "auth.password_reset", u.Email, nil)
	return nil
}

func (s *Service) localUser(ctx context.Context, email string) (store.User, bool) {
	var u store.User
	err := s.db.WithContext(ctx).Where("email = ? AND password_hash <> ''", email).Order("id").Take(&u).Error
	return u, err == nil
}

func (s *Service) sendAuthMail(ctx context.Context, userID int64, email, purpose, passwordHash string) error {
	ttl := confirmTTL
	subject := "Confirm your email"
	intro := "Confirm this address to finish creating your Easy Sys Admin account. The link works for 24 hours."
	if purpose == "reset" {
		ttl = resetTTL
		subject = "Reset your password"
		intro = "Use this link to choose a new password. It works for 1 hour and only once."
	} else if passwordHash != "" {
		subject = "Add a password to your account"
		intro = "Open this link to add a password to the Easy Sys Admin account for this address. It works for 24 hours and only once."
	}
	var token string
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		var err error
		token, err = s.issueAuthToken(ctx, q, userID, purpose, ttl, passwordHash)
		return err
	})
	if err != nil {
		return err
	}
	mode := "confirm"
	if purpose == "reset" {
		mode = "reset"
	}
	link := s.publicURL.ResolveReference(&url.URL{
		Path:     "/login",
		RawQuery: url.Values{"mode": {mode}, "token": {token}}.Encode(),
	}).String()
	body := intro + "\n\n" + link + "\n\nIf you did not ask for this, you can ignore this message.\n"
	return s.authMail.Send(ctx, email, subject, body)
}

func (s *Service) issueAuthToken(ctx context.Context, q *store.Queries, userID int64, purpose string, ttl time.Duration, passwordHash string) (string, error) {
	if err := q.Raw().WithContext(ctx).Where("user_id = ? AND purpose = ?", userID, purpose).Delete(&store.AuthEmailToken{}).Error; err != nil {
		return "", err
	}
	token, hash := randomToken("")
	row := store.AuthEmailToken{
		TokenHash: hash, UserID: userID, Purpose: purpose, PasswordHash: passwordHash,
		ExpiresAt: s.now().Add(ttl), CreatedAt: s.now(),
	}
	if err := q.Raw().WithContext(ctx).Create(&row).Error; err != nil {
		return "", err
	}
	return token, nil
}

func (s *Service) consumeAuthToken(ctx context.Context, token, purpose string) (store.User, string, error) {
	if token == "" || len(token) > 128 {
		return store.User{}, "", ErrAuthToken
	}
	var row store.AuthEmailToken
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		err := q.Raw().WithContext(ctx).Where("token_hash = ? AND purpose = ?", hashToken(token), purpose).Take(&row).Error
		if err != nil {
			return ErrAuthToken
		}
		if !s.now().Before(row.ExpiresAt) {
			_ = q.Raw().WithContext(ctx).Delete(&row).Error
			return ErrAuthToken
		}
		if err := q.Raw().WithContext(ctx).Delete(&row).Error; err != nil {
			return err
		}
		return q.Raw().WithContext(ctx).First(&row.User, row.UserID).Error
	})
	if err != nil {
		if errors.Is(err, ErrAuthToken) {
			return store.User{}, "", ErrAuthToken
		}
		return store.User{}, "", err
	}
	return row.User, row.PasswordHash, nil
}
