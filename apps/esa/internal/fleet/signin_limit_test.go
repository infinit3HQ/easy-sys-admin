package fleet

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSignInLimitIsSharedByTwoProcesses(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	other := &Service{db: s.db, log: s.log, now: s.now}
	const email, ip, otherIP = "ana@example.com", "1.1.1.1", "2.2.2.2"
	var last SignInTicket
	for range 5 {
		ticket, code := s.SignInReserve(ctx, email, ip)
		if code != "" {
			t.Fatalf("attempt before the limit: %q", code)
		}
		last = ticket
	}
	if _, got := other.SignInReserve(ctx, email, ip); got != "locked" {
		t.Fatalf("second process after 5 failures: %q", got)
	}
	if ticket, got := other.SignInReserve(ctx, email, otherIP); got != "" {
		t.Fatalf("same email from another address: %q", got)
	} else {
		other.SignInRelease(ticket)
	}
	for i := range 30 {
		other.SignInReserve(ctx, string(rune('a'+(i%26)))+string(rune('0'+i/26))+"@example.com", "3.3.3.3")
	}
	if _, got := s.SignInReserve(ctx, "new@example.com", "3.3.3.3"); got != "rate_limited" {
		t.Fatalf("client budget on the first process: %q", got)
	}
	s.SignInSucceed(last)
	if ticket, got := other.SignInReserve(ctx, email, ip); got != "" {
		t.Fatalf("after a successful sign-in: %q", got)
	} else {
		other.SignInRelease(ticket)
	}
	for range 10 {
		s.AuthHit("signup\n9.9.9.9")
	}
	if !other.AuthLimited("signup\n9.9.9.9", 10, time.Hour) {
		t.Fatal("a second process got its own sign-up quota")
	}
}

// A burst of parallel guesses is held to the same limit as a slow run of
// them, because each attempt is counted before its password is checked.
func TestSignInLimitHoldsUnderParallelGuesses(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t)
	const email, ip = "victim@example.com", "4.4.4.4"
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allowed int
	)
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, code := s.SignInReserve(ctx, email, ip); code == "" {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	// Some attempts may also be turned away because the server is busy;
	// what matters is that no more than the limit get through.
	if allowed < 1 || allowed > 5 {
		t.Fatalf("%d parallel guesses got through, want 1 to 5", allowed)
	}
}
