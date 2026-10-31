package fleet

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"strconv"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

type accountSigner struct {
	signer    ssh.Signer
	publicKey string
}

func keyContext(accountID int64) string { return "account_key:" + strconv.FormatInt(accountID, 10) }

// accountKey returns the account's SSH signer, creating the keypair on first use.
func (s *Service) accountKey(ctx context.Context, accountID int64) (*accountSigner, error) {
	s.signerMu.Lock()
	defer s.signerMu.Unlock()
	if k, ok := s.signers[accountID]; ok {
		return k, nil
	}

	var row store.AccountKey
	err := s.db.WithContext(ctx).Where("account_id = ?", accountID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row, err = s.createAccountKey(ctx, accountID)
	}
	if err != nil {
		return nil, err
	}
	pemBytes, err := s.box.Open(row.PrivateKeyEnc, keyContext(accountID))
	if err != nil {
		return nil, fmt.Errorf("open account key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("parse account key: %w", err)
	}
	k := &accountSigner{signer: signer, publicKey: row.PublicKey}
	s.signers[accountID] = k
	return k, nil
}

func (s *Service) createAccountKey(ctx context.Context, accountID int64) (store.AccountKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return store.AccountKey{}, err
	}
	block, err := ssh.MarshalPrivateKey(priv, "esa account "+strconv.FormatInt(accountID, 10))
	if err != nil {
		return store.AccountKey{}, err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return store.AccountKey{}, err
	}
	sealed, err := s.box.Seal(pem.EncodeToMemory(block), keyContext(accountID))
	if err != nil {
		return store.AccountKey{}, err
	}
	row := store.AccountKey{
		AccountID: accountID, PublicKey: remote.MarshalKey(sshPub), Fingerprint: remote.Fingerprint(sshPub),
		PrivateKeyEnc: sealed, CreatedAt: s.now(),
	}
	// Two requests may race to create the key; the first one wins.
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Omit(clause.Associations).Create(&row).Error; err != nil {
		return store.AccountKey{}, err
	}
	var stored store.AccountKey
	err = s.db.WithContext(ctx).Where("account_id = ?", accountID).Take(&stored).Error
	return stored, err
}

// AccountPublicKey returns the key the control plane logs in with, for the
// settings page and manual installs.
func (s *Service) AccountPublicKey(ctx context.Context, a Actor) (publicKey, fingerprint string, err error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return "", "", err
	}
	k, err := s.accountKey(ctx, a.AccountID)
	if err != nil {
		return "", "", err
	}
	return k.publicKey, remote.Fingerprint(k.signer.PublicKey()), nil
}

// endpoint builds the SSH endpoint for a ready server.
func (s *Service) endpoint(ctx context.Context, srv store.Server) (remote.Endpoint, error) {
	if srv.Enrollment != "ready" || srv.HostKey == "" {
		return remote.Endpoint{}, &UnavailableError{Message: srv.Name + " has not finished enrollment"}
	}
	hk, err := remote.ParseKey(srv.HostKey)
	if err != nil {
		return remote.Endpoint{}, fmt.Errorf("stored host key: %w", err)
	}
	k, err := s.accountKey(ctx, srv.AccountID)
	if err != nil {
		return remote.Endpoint{}, err
	}
	return remote.Endpoint{ServerID: srv.ID, Host: srv.Host, Port: srv.Port, User: srv.Username, HostKey: hk, Signer: k.signer}, nil
}
