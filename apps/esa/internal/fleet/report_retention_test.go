package fleet

import (
	"context"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestReportRetentionPreservesOpenNoticesAndLatestScans(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "report-retention")
	srv := lifecycleServer(t, s, a, "one")
	other := lifecycleServer(t, s, a, "two")
	old := s.now().Add(-100 * 24 * time.Hour)
	for _, state := range []string{"open", "acknowledged", "snoozed", "accepted", "resolved"} {
		row := store.SecurityIncident{AccountID: a.AccountID, ServerID: srv.ID, Kind: "new_port", Subject: state, State: state, Message: state, FirstSeen: old, LastSeen: old}
		if state == "resolved" {
			row.ResolvedAt = &old
		}
		if err := s.db.Omit("Account", "Server", "Assignee").Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, id := range []int64{srv.ID, srv.ID, other.ID, other.ID} {
		at := old.Add(time.Duration(i) * time.Hour)
		if i == 3 {
			at = s.now()
		}
		row := store.ScanImport{AccountID: a.AccountID, ServerID: id, Scanner: "grype", Matches: store.JSON("[]"), ImportedAt: at, ImportedBy: a.UserID}
		if err := s.db.Omit("Account", "Server").Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	count := func(model any) int64 {
		t.Helper()
		var n int64
		if err := s.db.Model(model).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	s.PruneReports(ctx)
	if count(&store.SecurityIncident{}) != 5 || count(&store.ScanImport{}) != 4 {
		t.Fatal("pruned without an explicit retention setting")
	}
	s.reportRetention = 90 * 24 * time.Hour
	s.auditHoldUntil = s.now().Add(time.Hour)
	s.PruneReports(ctx)
	if count(&store.ScanImport{}) != 4 {
		t.Fatal("audit hold did not protect report history")
	}
	s.auditHoldUntil = time.Time{}
	s.PruneReports(ctx)
	if count(&store.SecurityIncident{}) != 4 || count(&store.ScanImport{}) != 2 {
		t.Fatal("pruned an active notice or the latest scanner snapshot")
	}
	var latest []store.ScanImport
	s.db.Order("server_id").Find(&latest)
	if latest[0].ServerID != srv.ID || !latest[0].ImportedAt.Before(s.now().Add(-90*24*time.Hour)) {
		t.Fatal("stale latest evidence was erased")
	}
	second := &Service{store: s.store, db: s.db, log: s.log, now: s.now, reportRetention: s.reportRetention}
	second.PruneReports(ctx)
	if count(&store.RetentionRun{}) != 1 {
		t.Fatal("another process could prune the same hour")
	}
	s.openIncident(ctx, a.AccountID, srv.ID, "new_port", "resolved", "it came back", true)
	if count(&store.SecurityIncident{}) != 5 {
		t.Fatal("a recurrence after retention did not create a notice")
	}
}
