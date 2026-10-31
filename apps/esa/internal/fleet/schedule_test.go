package fleet

import (
	"testing"
	"time"
)

func TestScheduleLatestAndNext(t *testing.T) {
	sc, err := parseSchedule("sun,wed", "03:30", "Asia/Colombo")
	if err != nil {
		t.Fatal(err)
	}
	col, _ := time.LoadLocation("Asia/Colombo")
	// Saturday 31 Oct 2026, 17:00 in Colombo.
	now := time.Date(2026, 10, 31, 17, 0, 0, 0, col)
	if got, want := sc.Latest(now), time.Date(2026, 10, 28, 3, 30, 0, 0, col); !got.Equal(want) {
		t.Errorf("Latest = %v, want %v", got, want)
	}
	if got, want := sc.Next(now), time.Date(2026, 11, 1, 3, 30, 0, 0, col); !got.Equal(want) {
		t.Errorf("Next = %v, want %v", got, want)
	}
	// Exactly at the slot, Latest is the slot itself.
	at := time.Date(2026, 11, 1, 3, 30, 0, 0, col)
	if got := sc.Latest(at); !got.Equal(at) {
		t.Errorf("Latest at slot = %v", got)
	}
}

func TestScheduleAcrossDST(t *testing.T) {
	sc, err := parseSchedule("sun", "02:30", "Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	ber, _ := time.LoadLocation("Europe/Berlin")
	// 25 Oct 2026 is the switch from CEST to CET; 02:30 happens twice.
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, ber)
	got := sc.Latest(now)
	if got.In(ber).Day() != 25 || got.In(ber).Hour() != 2 || got.In(ber).Minute() != 30 {
		t.Fatalf("Latest = %v", got.In(ber))
	}
}

func TestParseScheduleRejects(t *testing.T) {
	bad := [][3]string{
		{"", "03:30", "UTC"},
		{"funday", "03:30", "UTC"},
		{"mon", "3:30", "UTC"},
		{"mon", "24:00", "UTC"},
		{"mon", "03:30", "Mars/Base"},
	}
	for _, b := range bad {
		if _, err := parseSchedule(b[0], b[1], b[2]); err == nil {
			t.Errorf("parseSchedule(%q, %q, %q) accepted", b[0], b[1], b[2])
		}
	}
}

func TestRolePermissions(t *testing.T) {
	viewer := Actor{Role: RoleViewer}
	admin := Actor{Role: RoleAdmin}
	readToken := Actor{Role: RoleOwner, Scopes: []Scope{ScopeRead}, TokenID: 1}
	if viewer.can(RoleAdmin, ScopeActions) == nil {
		t.Error("viewer must not run actions")
	}
	if viewer.can(RoleAdmin, ScopeLogs) == nil {
		t.Error("viewer must not read logs")
	}
	if admin.can(RoleAdmin, ScopeActions) != nil {
		t.Error("admin session may run actions")
	}
	if readToken.can(RoleViewer, ScopeRead) != nil {
		t.Error("read token may read")
	}
	if readToken.can(RoleAdmin, ScopeActions) == nil {
		t.Error("read token must not run actions, even for an owner")
	}
}
