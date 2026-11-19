package fleet

import (
	"testing"
	"time"
)

func TestSignInLimitIsSharedByTwoProcesses(t *testing.T) {
	s := newTestService(t)
	other := &Service{db: s.db, log: s.log, now: s.now}
	const email, ip, otherIP = "ana@example.com", "1.1.1.1", "2.2.2.2"
	for range 5 {
		s.SignInFail(email, ip)
	}
	if got := other.SignInBlocked(email, ip); got != "locked" {
		t.Fatalf("second process after 5 failures: %q", got)
	}
	if got := other.SignInBlocked(email, otherIP); got != "" {
		t.Fatalf("same email from another address: %q", got)
	}
	for i := range 30 {
		other.SignInFail(string(rune('a'+(i%26)))+string(rune('0'+i/26))+"@example.com", "3.3.3.3")
	}
	if got := s.SignInBlocked("new@example.com", "3.3.3.3"); got != "rate_limited" {
		t.Fatalf("client budget on the first process: %q", got)
	}
	s.SignInSucceed(email, ip)
	if got := other.SignInBlocked(email, ip); got != "" {
		t.Fatalf("after a successful sign-in: %q", got)
	}
	for range 10 {
		s.AuthHit("signup\n9.9.9.9")
	}
	if !other.AuthLimited("signup\n9.9.9.9", 10, time.Hour) {
		t.Fatal("a second process got its own sign-up quota")
	}
}
