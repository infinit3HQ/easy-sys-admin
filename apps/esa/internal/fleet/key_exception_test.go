package fleet

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestKeyExceptionHidesAnExtraKeyUntilItExpires(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "except")
	srv := addTestServer(t, s, a, nil, "app")
	baseFP := "SHA256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	base := []baselineKey{{User: "deploy", Fingerprint: baseFP}}
	if err := s.db.Omit("Server").Create(&store.KeyBaseline{ServerID: srv.ID, AccountID: a.AccountID, Items: store.MustJSON(base), ApprovedAt: s.now(), ApprovedBy: a.UserID}).Error; err != nil {
		t.Fatal(err)
	}
	fp := "SHA256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	seen := []keyUse{{User: "deploy", Fingerprint: baseFP}, {User: "deploy", Fingerprint: fp}}
	until := s.now().Add(48 * time.Hour)
	if _, err := s.CreateKeyException(ctx, a, srv.ID, "deploy", fp, "vendor key", until); err != nil {
		t.Fatal(err)
	}
	s.reportKeyDrift(ctx, srv, seen, true)
	var n int64
	if err := s.db.Model(&store.SecurityIncident{}).Where("server_id = ?", srv.ID).Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("exception still opened a notice: %d %v", n, err)
	}
	s.db.Model(&store.KeyException{}).Where("server_id = ?", srv.ID).Update("until", s.now().Add(-time.Hour))
	s.reportKeyDrift(ctx, srv, seen, true)
	var msg string
	if err := s.db.Model(&store.SecurityIncident{}).Where("server_id = ?", srv.ID).Pluck("message", &msg).Error; err != nil || msg == "" {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(msg), "revok") {
		t.Fatalf("the notice claims access was revoked: %s", msg)
	}
	if !strings.Contains(msg, "not in the approved baseline") {
		t.Fatalf("message: %s", msg)
	}
}

func TestKeyDriftResolvesWhenTheExtraKeyIsGone(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "key-back")
	srv := addTestServer(t, s, a, nil, "app")
	baseFP := "SHA256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	extra := "SHA256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := s.db.Omit("Server").Create(&store.KeyBaseline{ServerID: srv.ID, AccountID: a.AccountID, Items: store.MustJSON([]baselineKey{{User: "deploy", Fingerprint: baseFP}}), ApprovedAt: s.now(), ApprovedBy: a.UserID}).Error; err != nil {
		t.Fatal(err)
	}
	s.reportKeyDrift(ctx, srv, []keyUse{{User: "deploy", Fingerprint: baseFP}, {User: "deploy", Fingerprint: extra}}, false)
	var open int64
	if err := s.db.Model(&store.SecurityIncident{}).Where("server_id = ? AND kind = ? AND state <> ?", srv.ID, "new_key", "resolved").Count(&open).Error; err != nil || open != 1 {
		t.Fatalf("open %d %v", open, err)
	}
	s.reportKeyDrift(ctx, srv, []keyUse{{User: "deploy", Fingerprint: baseFP}}, false)
	if err := s.db.Model(&store.SecurityIncident{}).Where("server_id = ? AND kind = ? AND state <> ?", srv.ID, "new_key", "resolved").Count(&open).Error; err != nil || open != 1 {
		t.Fatalf("a partial check resolved the notice: %d %v", open, err)
	}
	s.reportKeyDrift(ctx, srv, []keyUse{{User: "deploy", Fingerprint: baseFP}}, true)
	if err := s.db.Model(&store.SecurityIncident{}).Where("server_id = ? AND kind = ? AND state <> ?", srv.ID, "new_key", "resolved").Count(&open).Error; err != nil || open != 0 {
		t.Fatalf("a complete check left the notice open: %d %v", open, err)
	}
}
