package fleet

import (
	"context"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestPruneAuditLeavesRecentRowsAndTheLedger(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	old := s.now().Add(-100 * 24 * time.Hour)
	recent := s.now().Add(-time.Hour)
	for _, at := range []time.Time{old, old, recent} {
		if err := s.db.Exec(`INSERT INTO audit_log (action, target, detail, source, created_at) VALUES ('server.add', '', '{}', 'web', ?)`, at).Error; err != nil {
			t.Fatal(err)
		}
	}
	s.auditRetention = 48 * time.Hour
	if got := s.countOldAudit(ctx, s.now().Add(-s.auditRetention)); got != 2 {
		t.Fatalf("eligible %d", got)
	}
	s.PruneAudit(ctx)
	var left int64
	if err := s.db.Model(&store.AuditEntry{}).Where("action = ?", "server.add").Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Fatalf("recent rows left: %d", left)
	}
	var runs int64
	if err := s.db.Model(&store.RetentionRun{}).Count(&runs).Error; err != nil || runs != 1 {
		t.Fatalf("ledger %d %v", runs, err)
	}
	var prune int64
	if err := s.db.Model(&store.AuditEntry{}).Where("action = ?", "audit.prune").Count(&prune).Error; err != nil || prune != 1 {
		t.Fatalf("prune audit %d %v", prune, err)
	}
	s.PruneAudit(ctx)
	if err := s.db.Model(&store.RetentionRun{}).Count(&runs).Error; err != nil || runs != 2 {
		t.Fatalf("second ledger %d %v", runs, err)
	}
	if err := s.db.Model(&store.AuditEntry{}).Where("action = ?", "server.add").Count(&left).Error; err != nil || left != 1 {
		t.Fatalf("second prune removed a recent row: %d %v", left, err)
	}
}

func TestOnlyOneProcessPrunesAudit(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	other := &Service{db: s.db, log: s.log, now: s.now}
	s.auditRetention = 48 * time.Hour
	other.auditRetention = s.auditRetention
	old := s.now().Add(-100 * 24 * time.Hour)
	if err := s.db.Exec(`INSERT INTO audit_log (action, target, detail, source, created_at) VALUES ('server.add', '', '{}', 'web', ?)`, old).Error; err != nil {
		t.Fatal(err)
	}
	s.PruneAudit(ctx)
	if err := s.db.Exec(`INSERT INTO audit_log (action, target, detail, source, created_at) VALUES ('server.add', 'later', '{}', 'web', ?)`, old).Error; err != nil {
		t.Fatal(err)
	}
	other.PruneAudit(ctx)
	var left int64
	if err := s.db.Model(&store.AuditEntry{}).Where("action = ?", "server.add").Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Fatalf("the second process pruned too: %d rows left", left)
	}
	var runs int64
	if err := s.db.Model(&store.RetentionRun{}).Count(&runs).Error; err != nil || runs != 1 {
		t.Fatalf("ledger %d %v", runs, err)
	}
}

func TestActivityShowsTheOldestKeptRow(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "audit-bound")
	old := s.now().Add(-10 * 24 * time.Hour).UTC().Truncate(time.Second)
	if err := s.db.Exec(`INSERT INTO audit_log (account_id, action, target, detail, source, created_at) VALUES (?, 'server.add', 'old', '{}', 'web', ?)`, a.AccountID, old).Error; err != nil {
		t.Fatal(err)
	}
	page, err := s.Activity(ctx, a, 10)
	if err != nil || page.Earliest == nil || !page.Earliest.Equal(old) {
		t.Fatalf("oldest %+v %v", page.Earliest, err)
	}
	if !page.KeptUntilDeletion || page.RetentionHours != 0 {
		t.Fatalf("default retention %+v", page)
	}
	s.auditRetention = 48 * time.Hour
	page, err = s.Activity(ctx, a, 10)
	if err != nil || page.KeptUntilDeletion || page.RetentionHours != 48 {
		t.Fatalf("set retention %+v %v", page, err)
	}
}

func TestAuditHoldSkipsPrune(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "hold")
	old := s.now().Add(-100 * 24 * time.Hour)
	if err := s.db.Exec(`INSERT INTO audit_log (account_id, action, target, detail, source, created_at) VALUES (?, 'server.add', 'old', '{}', 'web', ?)`, a.AccountID, old).Error; err != nil {
		t.Fatal(err)
	}
	s.auditRetention = 48 * time.Hour
	s.auditHoldUntil = s.now().Add(24 * time.Hour)
	s.PruneAudit(ctx)
	var left int64
	if err := s.db.Model(&store.AuditEntry{}).Where("action = ?", "server.add").Count(&left).Error; err != nil || left != 1 {
		t.Fatalf("hold removed a row: %d %v", left, err)
	}
	var runs int64
	if err := s.db.Model(&store.RetentionRun{}).Count(&runs).Error; err != nil || runs != 0 {
		t.Fatalf("hold wrote a ledger row: %d %v", runs, err)
	}
	page, err := s.Activity(ctx, a, 10)
	if err != nil || page.HoldUntil == nil || !page.HoldUntil.Equal(s.auditHoldUntil) {
		t.Fatalf("hold not shown: %+v %v", page.HoldUntil, err)
	}
	s.auditHoldUntil = s.now().Add(-time.Hour)
	s.PruneAudit(ctx)
	if err := s.db.Model(&store.AuditEntry{}).Where("action = ?", "server.add").Count(&left).Error; err != nil || left != 0 {
		t.Fatalf("expired hold still kept the row: %d %v", left, err)
	}
}
