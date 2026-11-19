package fleet

import (
	"context"
	"testing"
)

func TestSubmittedRecoveryDoesNotReplaceANewerFailure(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "ingest")
	srv := addTestServer(t, s, a, nil, "db")
	fail := []byte(`{"schema":1,"tool":"restic","dataset":"postgres","snapshot":"new","backup":{"state":"failed","at":"2026-11-14T12:00:00Z"}}`)
	early := []byte(`{"schema":1,"tool":"restic","dataset":"postgres","snapshot":"old","backup":{"state":"success","at":"2026-11-01T00:00:00Z"}}`)
	if err := s.SubmitRecovery(ctx, a, srv.ID, "failure-key", fail); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitRecovery(ctx, a, srv.ID, "failure-key", early); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitRecovery(ctx, a, srv.ID, "early-success", early); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitRecovery(ctx, a, srv.ID, "bad", []byte(`{"schema":1,"tool":"restic","dataset":"db","snapshot":"s3://x"}`)); err == nil {
		t.Fatal("a repository URL was stored")
	}
	var rows []struct {
		BackupState    string
		IdempotencyKey string
	}
	if err := s.db.Table("ingested_recoveries").Where("server_id = ?", srv.ID).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].BackupState != "failed" || rows[0].IdempotencyKey != "failure-key" || rows[1].BackupState != "success" {
		t.Fatalf("rows: %+v", rows)
	}
	viewer := a
	viewer.Role = RoleViewer
	if err := s.SubmitRecovery(ctx, viewer, srv.ID, "viewer-key1", fail); err == nil {
		t.Fatal("a viewer stored a result")
	}
}
