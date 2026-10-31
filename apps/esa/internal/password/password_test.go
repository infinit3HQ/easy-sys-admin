package password

import (
	"context"
	"errors"
	"testing"
)

func TestHashAndCheck(t *testing.T) {
	ctx := context.Background()
	h, err := Hash(ctx, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := Check(ctx, h, "correct horse battery"); !ok || err != nil {
		t.Fatalf("right password: %v, %v", ok, err)
	}
	if ok, err := Check(ctx, h, "wrong"); ok || err != nil {
		t.Fatalf("wrong password: %v, %v", ok, err)
	}
	if _, err := Check(ctx, "nonsense", "x"); err == nil {
		t.Fatal("malformed hash accepted")
	}
}

func TestSlotsBounded(t *testing.T) {
	// Fill every slot, then a cancelled wait must fail instead of hashing.
	for range cap(slots) {
		slots <- struct{}{}
	}
	defer func() {
		for range cap(slots) {
			<-slots
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Hash(ctx, "correct horse battery"); !errors.Is(err, context.Canceled) {
		t.Fatalf("hash with no free slot: %v", err)
	}
	if _, err := Check(ctx, Dummy, "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("check with no free slot: %v", err)
	}
}
