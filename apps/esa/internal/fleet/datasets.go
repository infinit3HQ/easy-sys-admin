package fleet

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// DatasetView is one named backup dataset and how old its last success may be.
type DatasetView struct {
	ID          int64
	Name        string
	MaxAgeHours int
}

func (s *Service) ListDatasets(ctx context.Context, a Actor) ([]DatasetView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	var rows []store.DatasetPolicy
	if err := s.db.WithContext(ctx).Where("account_id = ?", a.AccountID).Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]DatasetView, 0, len(rows))
	for _, r := range rows {
		out = append(out, DatasetView{ID: r.ID, Name: r.Name, MaxAgeHours: r.MaxAgeHours})
	}
	return out, nil
}

func (s *Service) CreateDataset(ctx context.Context, a Actor, name string, maxAgeHours int) (DatasetView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return DatasetView{}, err
	}
	name, err := datasetName(name)
	if err != nil {
		return DatasetView{}, err
	}
	if maxAgeHours < 1 || maxAgeHours > 2160 {
		return DatasetView{}, invalid("maxAgeHours", "must be from 1 to 2160")
	}
	row := store.DatasetPolicy{AccountID: a.AccountID, Name: name, MaxAgeHours: maxAgeHours, CreatedAt: s.now()}
	if err := s.db.WithContext(ctx).Omit("Account").Create(&row).Error; err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "UNIQUE") {
			return DatasetView{}, &ConflictError{Message: "a dataset with that name already exists"}
		}
		return DatasetView{}, err
	}
	s.audit(ctx, nil, a, "dataset.create", name, map[string]any{"maxAgeHours": maxAgeHours})
	return DatasetView{ID: row.ID, Name: row.Name, MaxAgeHours: row.MaxAgeHours}, nil
}

func (s *Service) DeleteDataset(ctx context.Context, a Actor, id int64) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	var row store.DatasetPolicy
	if err := s.db.WithContext(ctx).Where("id = ? AND account_id = ?", id, a.AccountID).Take(&row).Error; err != nil {
		return mapStore(err)
	}
	if err := s.db.WithContext(ctx).Delete(&row).Error; err != nil {
		return err
	}
	s.audit(ctx, nil, a, "dataset.delete", row.Name, nil)
	return nil
}

func datasetName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > 80 || strings.Contains(s, "://") || strings.Contains(s, "@") || strings.ContainsAny(s, " \t\n/") {
		return "", invalid("name", "use 1 to 80 characters, with no spaces or a repository URL")
	}
	return s, nil
}

// applyDatasetPolicy sets each source's policy from the owning account.
// A dataset with no policy is none, which is not a current backup.
func (s *Service) applyDatasetPolicy(ctx context.Context, accountID int64, rep *proto.RecoveryReport) {
	var rows []store.DatasetPolicy
	if err := s.db.WithContext(ctx).Where("account_id = ?", accountID).Find(&rows).Error; err != nil {
		return
	}
	by := map[string]store.DatasetPolicy{}
	for _, r := range rows {
		by[r.Name] = r
	}
	now := s.now()
	for i := range rep.Sources {
		p, ok := by[rep.Sources[i].Dataset]
		if !ok {
			rep.Sources[i].Policy = "none"
			continue
		}
		at := rep.Sources[i].BackupAt
		fresh := at != nil && !at.After(now.Add(2*time.Minute)) && now.Sub(*at) <= time.Duration(p.MaxAgeHours)*time.Hour
		okState := rep.Sources[i].BackupState == "success" || rep.Sources[i].BackupState == "warning"
		if fresh && okState {
			rep.Sources[i].Policy = "within"
			continue
		}
		rep.Sources[i].Policy = "older"
	}
}
