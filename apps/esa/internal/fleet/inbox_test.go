package fleet

import (
	"context"
	"strings"
	"testing"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

type quietNotifier struct{}

func (quietNotifier) Send(context.Context, store.NotifyChannel, []byte, Message) error {
	return nil
}

func TestRepeatedPortIsOneIncidentAndOneNotice(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "inbox")
	if _, err := s.CreateChannel(ctx, a, "desk", "webhook", ChannelConfig{URL: "https://hook.example.com/inbox"}); err != nil {
		t.Fatal(err)
	}
	s.notifier = quietNotifier{}
	one := addTestServer(t, s, a, nil, "one")
	two := addTestServer(t, s, a, nil, "two")
	rules, err := s.rules(ctx, a.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	port := []string{"0.0.0.0:22"}
	s.oneShot(ctx, one, rules[AlertNewPort], AlertNewPort, port, "new socket on all interfaces")
	s.oneShot(ctx, one, rules[AlertNewPort], AlertNewPort, port, "new socket on all interfaces")
	s.oneShot(ctx, two, rules[AlertNewPort], AlertNewPort, port, "new socket on all interfaces")

	var incidents []store.SecurityIncident
	if err := s.db.Where("account_id = ? AND kind = ?", a.AccountID, AlertNewPort).Find(&incidents).Error; err != nil {
		t.Fatal(err)
	}
	if len(incidents) != 2 {
		t.Fatalf("incidents = %d, want one per server", len(incidents))
	}
	var deliveries int64
	if err := s.db.Model(&store.NotifyDelivery{}).Where("account_id = ?", a.AccountID).Count(&deliveries).Error; err != nil {
		t.Fatal(err)
	}
	if deliveries != 2 {
		t.Fatalf("notices = %d, want one per server", deliveries)
	}
	var alerts int64
	if err := s.db.Model(&store.Alert{}).Where("account_id = ?", a.AccountID).Count(&alerts).Error; err != nil {
		t.Fatal(err)
	}
	if alerts != 2 {
		t.Fatalf("alert rows = %d, want one per server", alerts)
	}
	views, err := s.ListIncidents(ctx, a, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || views[0].NextStep == "" || !strings.Contains(views[0].NextStep, "not a reachability result") {
		t.Fatalf("next step missing: %+v", views)
	}

	if _, err := s.AcknowledgeIncident(ctx, a, incidents[0].ID); err != nil {
		t.Fatal(err)
	}
	s.oneShot(ctx, one, rules[AlertNewPort], AlertNewPort, port, "new socket on all interfaces")
	var again store.SecurityIncident
	if err := s.db.First(&again, incidents[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if again.State != "acknowledged" {
		t.Fatalf("state = %s, review was cleared", again.State)
	}
	if err := s.db.Model(&store.NotifyDelivery{}).Where("account_id = ?", a.AccountID).Count(&deliveries).Error; err != nil {
		t.Fatal(err)
	}
	if deliveries != 2 {
		t.Fatalf("notices after review = %d, want 2", deliveries)
	}
	if err := s.db.Model(&store.Alert{}).Where("account_id = ?", a.AccountID).Count(&alerts).Error; err != nil {
		t.Fatal(err)
	}
	if alerts != 2 {
		t.Fatalf("alert rows after repeat = %d, want 2", alerts)
	}
}
