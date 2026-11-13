package fleet

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestSummarySlot(t *testing.T) {
	now := time.Date(2026, 11, 13, 2, 30, 0, 0, time.UTC) // 08:00 in Colombo
	slot, ok := summarySlot(now, 8, "Asia/Colombo")
	if !ok || !slot.Equal(time.Date(2026, 11, 13, 2, 30, 0, 0, time.UTC)) {
		t.Fatalf("slot %v", slot)
	}
	slot, _ = summarySlot(now.Add(-time.Minute), 8, "Asia/Colombo")
	if !slot.Equal(time.Date(2026, 11, 12, 2, 30, 0, 0, time.UTC)) {
		t.Fatalf("before the hour the slot is yesterday's: %v", slot)
	}
	if _, ok := summarySlot(now, 8, "Not/AZone"); ok {
		t.Fatal("an unknown zone has no slot")
	}
}

func deliveries(t *testing.T, s *Service, accountID int64) []store.NotifyDelivery {
	t.Helper()
	var rows []store.NotifyDelivery
	if err := s.db.Where("account_id = ?", accountID).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestDailyWarningsWaitAndCriticalDoesNot(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "routing")
	if _, err := s.CreateChannel(ctx, a, "desk", "webhook", ChannelConfig{URL: "https://hook.example.com/routing"}); err != nil {
		t.Fatal(err)
	}
	s.notifier = quietNotifier{}
	if _, err := s.SetRouting(ctx, a, Routing{Warnings: DeliverDaily, Hour: 8, Timezone: "Bogus/Zone"}); err == nil {
		t.Fatal("an unknown zone was accepted")
	}
	if _, err := s.SetRouting(ctx, a, Routing{Warnings: DeliverDaily, Hour: 8, Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	s.notify(ctx, a.AccountID, Message{Title: "web: disk / is 85% full", Severity: "warning"})
	s.notify(ctx, a.AccountID, Message{Title: "db: disk / is 91% full", Severity: "warning"})
	s.notify(ctx, a.AccountID, Message{Title: "web: new SSH key", Severity: "warning", Kind: string(AlertNewKey)})
	s.notify(ctx, a.AccountID, Message{Title: "web: not answering", Severity: "critical"})
	if got := deliveries(t, s, a.AccountID); len(got) != 2 {
		t.Fatalf("only the key and the critical notice go at once, got %d", len(got))
	}
	s.SendWarningSummaries(ctx)
	s.SendWarningSummaries(ctx)
	got := deliveries(t, s, a.AccountID)
	if len(got) != 3 || !strings.HasPrefix(got[2].Title, "2 warnings since") || !strings.Contains(got[2].Body, "db: disk / is 91% full") {
		t.Fatalf("one summary of two warnings, once: %+v", got)
	}
	var held int64
	s.db.Model(&store.HeldWarning{}).Where("account_id = ?", a.AccountID).Count(&held)
	if held != 0 {
		t.Fatalf("held warnings left: %d", held)
	}
	s.notify(ctx, a.AccountID, Message{Title: "web: memory 92%", Severity: "warning"})
	if _, err := s.SetRouting(ctx, a, Routing{Warnings: DeliverImmediate, Hour: 8, Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	if got := deliveries(t, s, a.AccountID); len(got) != 4 {
		t.Fatalf("switching to immediate sends what was held, got %d", len(got))
	}
}

func TestRetryOnlyDeadDeliveries(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "retry")
	ch, err := s.CreateChannel(ctx, a, "desk", "webhook", ChannelConfig{URL: "https://hook.example.com/retry"})
	if err != nil {
		t.Fatal(err)
	}
	dead := store.NotifyDelivery{AccountID: a.AccountID, ChannelID: ch.ID, Title: "x", Body: "x", Severity: "warning", State: "dead", Attempts: 5, CreatedAt: time.Now()}
	sent := store.NotifyDelivery{AccountID: a.AccountID, ChannelID: ch.ID, Title: "y", Body: "y", Severity: "warning", State: "sent", Attempts: 1, CreatedAt: time.Now()}
	for _, d := range []*store.NotifyDelivery{&dead, &sent} {
		if err := s.db.Omit("Account", "Channel").Create(d).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RetryDelivery(ctx, a, sent.ID); err == nil {
		t.Fatal("a sent delivery was retried")
	}
	other := signIn(t, s, "retry-other")
	if err := s.RetryDelivery(ctx, other, dead.ID); err == nil {
		t.Fatal("another space retried this delivery")
	}
	if err := s.RetryDelivery(ctx, a, dead.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListDeliveries(ctx, a, false)
	if err != nil || len(list) != 2 || list[0].Channel != "desk" {
		t.Fatalf("list: %+v %v", list, err)
	}
	s.Wait()
}
