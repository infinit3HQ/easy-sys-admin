package fleet

import (
	"context"
	"testing"
)

func TestServerLockIsHeldAcrossConnections(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	unlock, ok := s.tryServerLock(ctx, 42)
	if !ok {
		t.Fatal("first lock")
	}
	if _, ok := s.tryServerLock(ctx, 42); ok {
		t.Fatal("a second connection took the same server lock")
	}
	unlock()
	unlock, ok = s.tryServerLock(ctx, 42)
	if !ok {
		t.Fatal("lock was not released")
	}
	unlock()
}
