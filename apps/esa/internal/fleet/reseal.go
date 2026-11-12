package fleet

import (
	"context"
	"fmt"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/crypto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// ResealClass is one kind of sealed secret.
type ResealClass struct {
	Name     string
	Total    int
	Current  int
	Resealed int
	Failed   int
	Left     int
}

// ResealReport is the operator view of a re-seal. It has key ids and counts,
// never key bytes or plaintext.
type ResealReport struct {
	CurrentKey string
	Classes    []ResealClass
	Ready      bool
}

// Reseal rewrites sealed secrets with the current master key. dry counts
// what would change and writes nothing. A row that cannot be opened is left
// as it is and counted as failed.
func (s *Service) Reseal(ctx context.Context, dry bool) (ResealReport, error) {
	if s.box == nil {
		return ResealReport{}, fmt.Errorf("no master key")
	}
	rep := ResealReport{CurrentKey: s.box.Current(), Ready: true}
	keys, err := s.resealAccountKeys(ctx, dry)
	if err != nil {
		return rep, err
	}
	logins, err := s.resealLogins(ctx, dry)
	if err != nil {
		return rep, err
	}
	channels, err := s.resealChannels(ctx, dry)
	if err != nil {
		return rep, err
	}
	rep.Classes = []ResealClass{keys, logins, channels}
	for _, c := range rep.Classes {
		if c.Failed > 0 || c.Left > 0 {
			rep.Ready = false
		}
	}
	return rep, nil
}

func (s *Service) resealAccountKeys(ctx context.Context, dry bool) (ResealClass, error) {
	var rows []store.AccountKey
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return ResealClass{}, err
	}
	out := ResealClass{Name: "account keys", Total: len(rows)}
	for _, row := range rows {
		s.resealOne(&out, dry, row.PrivateKeyEnc, keyContext(row.AccountID), func(next, prev string) error {
			res := s.db.WithContext(ctx).Model(&store.AccountKey{}).
				Where("account_id = ? AND private_key_enc = ?", row.AccountID, prev).
				Update("private_key_enc", next)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return errResealRaced
			}
			return nil
		})
	}
	return out, nil
}

func (s *Service) resealLogins(ctx context.Context, dry bool) (ResealClass, error) {
	var rows []store.ServerCredential
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return ResealClass{}, err
	}
	out := ResealClass{Name: "server logins", Total: len(rows)}
	for _, row := range rows {
		s.resealOne(&out, dry, row.SecretEnc, loginContext(row.ServerID), func(next, prev string) error {
			res := s.db.WithContext(ctx).Model(&store.ServerCredential{}).
				Where("server_id = ? AND secret_enc = ?", row.ServerID, prev).
				Update("secret_enc", next)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return errResealRaced
			}
			return nil
		})
	}
	return out, nil
}

func (s *Service) resealChannels(ctx context.Context, dry bool) (ResealClass, error) {
	var rows []store.NotifyChannel
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return ResealClass{}, err
	}
	out := ResealClass{Name: "notification channels", Total: len(rows)}
	for _, row := range rows {
		s.resealOne(&out, dry, row.ConfigEnc, channelContext(row.ID), func(next, prev string) error {
			res := s.db.WithContext(ctx).Model(&store.NotifyChannel{}).
				Where("id = ? AND config_enc = ?", row.ID, prev).
				Update("config_enc", next)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return errResealRaced
			}
			return nil
		})
	}
	return out, nil
}

var errResealRaced = fmt.Errorf("row changed")

func (s *Service) resealOne(out *ResealClass, dry bool, sealed, context string, write func(next, prev string) error) {
	id, err := crypto.KeyID(sealed)
	if err != nil {
		out.Failed++
		return
	}
	if id == s.box.Current() {
		out.Current++
		return
	}
	if dry {
		if _, err := s.box.Open(sealed, context); err != nil {
			out.Failed++
			return
		}
		out.Left++
		return
	}
	next, changed, err := s.box.Reseal(sealed, context)
	if err != nil || !changed {
		out.Failed++
		return
	}
	if err := write(next, sealed); err != nil {
		out.Left++
		return
	}
	out.Resealed++
}
