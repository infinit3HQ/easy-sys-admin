package fleet

import (
	"context"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

func TestDatasetPolicyDoesNotCallAMissCurrent(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "datasets")
	if _, err := s.CreateDataset(ctx, a, "s3://bucket", 24); err == nil {
		t.Fatal("a repository URL was accepted as a dataset name")
	}
	got, err := s.CreateDataset(ctx, a, "postgres", 24)
	if err != nil || got.Name != "postgres" || got.MaxAgeHours != 24 {
		t.Fatalf("create: %+v %v", got, err)
	}
	viewer := a
	viewer.Role = RoleViewer
	if _, err := s.CreateDataset(ctx, viewer, "other", 24); err == nil {
		t.Fatal("a viewer created a policy")
	}
	recent := s.now().Add(-time.Hour)
	old := s.now().Add(-48 * time.Hour)
	rep := proto.RecoveryReport{Sources: []proto.RecoverySource{
		{Dataset: "postgres", BackupState: "success", BackupAt: &recent},
		{Dataset: "postgres", BackupState: "failed", BackupAt: &recent},
		{Dataset: "mail", BackupState: "success", BackupAt: &recent},
		{Dataset: "postgres", BackupState: "success", BackupAt: &old},
	}}
	s.applyDatasetPolicy(ctx, a.AccountID, &rep)
	want := []string{"within", "older", "none", "older"}
	for i, w := range want {
		if rep.Sources[i].Policy != w {
			t.Fatalf("source %d policy %s, want %s", i, rep.Sources[i].Policy, w)
		}
	}
}
