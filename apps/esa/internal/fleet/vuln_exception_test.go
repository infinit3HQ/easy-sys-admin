package fleet

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestVulnExceptionStaysInTheListUntilItExpires(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "vuln-ex")
	srv := addTestServer(t, s, a, nil, "web")
	raw := strings.Replace(grypeFixture, "2026-11-12T03:00:00Z", s.now().UTC().Format(time.RFC3339), 1)
	if _, err := s.ImportScan(ctx, a, srv.ID, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	until := s.now().Add(24 * time.Hour)
	if _, err := s.CreateVulnException(ctx, a, srv.ID, "CVE-2026-2222", "openssl", "wait for the window", until); err != nil {
		t.Fatal(err)
	}
	v, err := s.LatestScan(ctx, a, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, m := range v.Matches {
		if m.ID == "CVE-2026-2222" && m.Package == "openssl" {
			found = m.ExceptedUntil != nil
		}
	}
	if !found || !strings.Contains(v.Coverage, "not a fix") {
		t.Fatalf("exception did not stay visible: %+v", v.Matches)
	}
	if err := s.db.Model(&struct{}{}).Table("vuln_exceptions").Where("server_id = ?", srv.ID).Update("until", s.now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	v, err = s.LatestScan(ctx, a, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range v.Matches {
		if m.ID == "CVE-2026-2222" && m.ExceptedUntil != nil {
			t.Fatal("an expired exception still applies")
		}
	}
}
