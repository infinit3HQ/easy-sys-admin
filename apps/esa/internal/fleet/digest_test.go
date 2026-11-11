package fleet

import (
	"strings"
	"testing"
	"time"
)

func TestDigestDue(t *testing.T) {
	monday := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC) // Monday
	if !digestDue(monday, nil) {
		t.Fatal("a Monday with no previous summary is due")
	}
	sent := monday.Add(time.Hour)
	if digestDue(monday.Add(2*time.Hour), &sent) {
		t.Fatal("a summary already sent this Monday is not due again")
	}
	sunday := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	if digestDue(sunday, nil) {
		t.Fatal("Sunday is not the Monday slot")
	}
	early := time.Date(2026, 11, 9, 7, 0, 0, 0, time.UTC)
	if digestDue(early, nil) {
		t.Fatal("before 08:00 UTC is not due")
	}
}

func TestDigestText(t *testing.T) {
	now := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	if _, _, ok := digestText(now, digestFacts{}); ok {
		t.Fatal("an empty week sends nothing")
	}
	title, body, ok := digestText(now, digestFacts{Unreachable: []string{"db-1"}, Waiting: []string{"web openssl for 12 days"}})
	if !ok || title != "Week of 9 Nov" || !strings.Contains(body, "db-1") || !strings.Contains(body, "openssl") {
		t.Fatalf("title %q body %q", title, body)
	}
}
