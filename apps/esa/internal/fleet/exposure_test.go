package fleet

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestSummarizeExposure(t *testing.T) {
	at := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)
	access := proto.AccessReport{
		Revision: proto.ReportRevision, SSHRead: true, PasswordAuth: true, PermitRootLogin: "no",
		PubkeyAuth: true,
		Users: []proto.LocalUser{
			{Name: "root", UID: 0},
			{Name: "toor", UID: 0},
		},
		SudoRules: []proto.SudoRule{
			{Who: "esa", NoPassword: true, Commands: "/usr/local/lib/esa/esa-helper *"},
			{Who: "%sudo", NoPassword: true, Unrestricted: true, Commands: "ALL"},
		},
	}
	sec := proto.SecurityReport{
		Revision: proto.ReportRevision, Firewall: "none",
		Listening: []proto.Listener{
			{Proto: "tcp", Port: 22, Exposure: "all"},
			{Proto: "tcp6", Port: 22, Exposure: "all"},
			{Proto: "tcp", Port: 443, Exposure: "all"},
			{Proto: "tcp", Port: 53, Exposure: "local"},
			{Proto: "tcp", Port: 5432, Address: "10.0.0.5", Exposure: "other"},
		},
	}
	ex := summarizeExposure(access, sec, at)
	if !ex.Ready || !ex.PasswordAuth || ex.Firewall != "none" || ex.ExtraRoot != 1 || ex.NoPasswordAll != 1 {
		t.Fatalf("summary: %+v", ex)
	}
	if len(ex.PublicPorts) != 2 || ex.PublicPorts[0] != "22/tcp" || ex.PublicPorts[1] != "443/tcp" {
		t.Fatalf("ports: %+v", ex.PublicPorts)
	}
	if len(ex.SpecificBinds) != 1 || ex.SpecificBinds[0] != "10.0.0.5 5432/tcp" {
		t.Fatalf("specific binds: %+v", ex.SpecificBinds)
	}
	oldRev := summarizeExposure(proto.AccessReport{Revision: 1}, proto.SecurityReport{Revision: 1}, at)
	if oldRev.Ready {
		t.Fatal("a helper that does not speak the current report must not look ready")
	}
	kept := mergeExposure(ex, oldRev)
	if !kept.CheckedAt.Equal(ex.CheckedAt) || len(kept.PublicPorts) != 2 || kept.Ready || !hasGap(kept.Gaps, "helper") {
		t.Fatalf("an old helper must not wipe the last good summary: %+v", kept)
	}
	old := summarizeExposure(proto.AccessReport{}, proto.SecurityReport{Listening: []proto.Listener{{Exposure: "all", Port: 22, Proto: "tcp"}}}, at)
	if old.Ready || len(old.PublicPorts) != 0 || old.PasswordAuth {
		t.Fatalf("an old helper must not look clean: %+v", old)
	}
	if !exposureChanged(ex, Exposure{Ready: true, Firewall: "ufw active", PublicPorts: []string{}}) {
		t.Fatal("a firewall and port change must be recorded")
	}
	if got := added([]string{"22/tcp"}, []string{"22/tcp", "8080/tcp"}); len(got) != 1 || got[0] != "8080/tcp" {
		t.Fatalf("added ports: %#v", got)
	}
	if len(added(nil, []string{"22/tcp"})) != 1 {
		t.Fatal("everything is new when there is no baseline")
	}
	oldSeen := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	seen := mergeSecuritySeen(map[string]time.Time{"openssl": oldSeen}, proto.PendingUpdates{Packages: []proto.PendingPackage{
		{Name: "openssl", Security: true},
		{Name: "bash", Security: false},
		{Name: "libc6", Security: true},
	}}, at)
	if !seen["openssl"].Equal(oldSeen) || seen["libc6"].IsZero() || seen["bash"] != (time.Time{}) {
		t.Fatalf("seen: %+v", seen)
	}
	if _, ok := securityUpdateCondition("web", seen, at, 7); !ok {
		t.Fatal("openssl has been waiting since October")
	}
	if _, ok := securityUpdateCondition("web", map[string]time.Time{"openssl": at}, at, 7); ok {
		t.Fatal("a package seen today must not alert at 7 days")
	}
	if exposureChanged(Exposure{}, ex) {
		t.Fatal("the first summary is not a change")
	}
	conds := exposureConditions(ex)
	if len(conds) != 4 {
		t.Fatalf("conditions: %+v", conds)
	}
}

func TestGoneSocketResolvesOnlyOnACompleteRead(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "ports")
	srv := addTestServer(t, s, a, nil, "web")
	s.openIncident(ctx, a.AccountID, srv.ID, string(AlertNewPort), "22/tcp", "web: 22/tcp", true)
	s.openIncident(ctx, a.AccountID, srv.ID, string(AlertNewPort), "this check", "web: several sockets", true)
	s.openIncident(ctx, a.AccountID, srv.ID, string(AlertNewPort), "203.0.113.5 5432/tcp", "web: database socket", true)
	other := lifecycleServer(t, s, a, "other")
	s.openIncident(ctx, a.AccountID, other.ID, string(AlertNewPort), "22/tcp", "other: 22/tcp", true)
	s.openIncident(ctx, a.AccountID, srv.ID, string(AlertNewKey), "key", "web: key", true)
	s.noteAdditions(ctx, srv, nil, nil, nil, nil, false)
	var open int64
	q := s.db.Model(&store.SecurityIncident{}).Where("server_id = ? AND kind = ? AND state <> ?", srv.ID, string(AlertNewPort), "resolved")
	if err := q.Count(&open).Error; err != nil || open != 3 {
		t.Fatalf("partial read resolved sockets: %d %v", open, err)
	}
	ports := []string{"203.0.113.5 5432/tcp"}
	s.noteAdditions(ctx, srv, ports, ports, nil, nil, true)
	if err := s.db.Model(&store.SecurityIncident{}).Where("server_id = ? AND kind = ? AND subject = ? AND state <> ?", srv.ID, string(AlertNewPort), "22/tcp", "resolved").Count(&open).Error; err != nil || open != 0 {
		t.Fatalf("22/tcp stayed open: %d %v", open, err)
	}
	if err := s.db.Model(&store.SecurityIncident{}).Where("server_id = ? AND subject = ? AND state <> ?", srv.ID, "this check", "resolved").Count(&open).Error; err != nil || open != 1 {
		t.Fatalf("grouped notice was resolved: %d %v", open, err)
	}
	if err := q.Count(&open).Error; err != nil || open != 2 {
		t.Fatalf("a socket still present was resolved: %d %v", open, err)
	}
	s.noteAdditions(ctx, srv, ports, nil, nil, nil, true)
	if err := q.Count(&open).Error; err != nil || open != 1 {
		t.Fatalf("specific bind did not resolve: %d %v", open, err)
	}
	var untouched int64
	if err := s.db.Model(&store.SecurityIncident{}).Where("state <> ? AND (server_id = ? OR kind = ?)", "resolved", other.ID, AlertNewKey).Count(&untouched).Error; err != nil || untouched != 2 {
		t.Fatalf("another server or kind changed: %d %v", untouched, err)
	}
}

func TestGroupedSocketNoticeResolvesOnlyAfterEverySocketIsGone(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a := signIn(t, s, "grouped-sockets")
	srv := lifecycleServer(t, s, a, "web")
	if _, err := s.CreateChannel(ctx, a, "desk", "webhook", ChannelConfig{URL: "https://hook.example.com/sockets"}); err != nil {
		t.Fatal(err)
	}
	s.notifier = quietNotifier{}
	rules, err := s.rules(ctx, a.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	var ports []string
	for port := 8000; port < 8010; port++ {
		ports = append(ports, fmt.Sprintf("%d/tcp", port))
	}
	s.oneShot(ctx, srv, rules[AlertNewPort], AlertNewPort, ports, "new listening socket")
	var row store.SecurityIncident
	if err := s.db.Where("server_id = ? AND kind = ?", srv.ID, AlertNewPort).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row.Message, ports[9]) {
		t.Fatal("test needs a socket omitted from the shortened message")
	}
	s.resolveGonePorts(ctx, srv, ports[9:])
	if v := incidentState(t, s, a, row.ID); v.State != "open" {
		t.Fatal("a socket omitted from the message was forgotten")
	}
	// A later batch uses the same grouped notice. It must keep the earlier
	// sockets too, and all of this evidence must survive a database read.
	more := []string{"9000/tcp", "9001/udp"}
	s.oneShot(ctx, srv, rules[AlertNewPort], AlertNewPort, more, "new listening socket")
	s.resolveGonePorts(ctx, srv, ports[9:])
	if v := incidentState(t, s, a, row.ID); v.State != "open" {
		t.Fatal("a later batch erased an earlier socket")
	}
	s.resolveGonePorts(ctx, srv, more[1:])
	if v := incidentState(t, s, a, row.ID); v.State != "open" {
		t.Fatal("a later socket was forgotten")
	}
	s.noteAdditions(ctx, srv, nil, nil, nil, nil, false)
	if v := incidentState(t, s, a, row.ID); v.State != "open" {
		t.Fatal("a partial read resolved the group")
	}
	s.noteAdditions(ctx, srv, nil, nil, nil, nil, true)
	s.noteAdditions(ctx, srv, nil, nil, nil, nil, true)
	v := incidentState(t, s, a, row.ID)
	if v.State != "resolved" || v.ResolvedAt == nil || len(v.Events) != 2 || v.Events[1].Kind != eventResolved {
		t.Fatalf("resolution and history: %+v", v)
	}
	var deliveries int64
	if err := s.db.Model(&store.NotifyDelivery{}).Where("account_id = ?", a.AccountID).Count(&deliveries).Error; err != nil || deliveries != 2 {
		t.Fatalf("want one opening and one resolution notification: %d %v", deliveries, err)
	}
	s.oneShot(ctx, srv, rules[AlertNewPort], AlertNewPort, ports[:2], "new listening socket")
	if v := incidentState(t, s, a, row.ID); v.State != "open" || v.Recurrences != 1 {
		t.Fatalf("recurrence: %+v", v)
	}
	// The previous occurrence's sockets must not hold the new one open.
	s.resolveGonePorts(ctx, srv, ports[9:])
	if v := incidentState(t, s, a, row.ID); v.State != "resolved" {
		t.Fatal("a recurrence kept sockets from the previous occurrence")
	}
}

func TestFailedSocketReadDoesNotSeedAnEmptyBaseline(t *testing.T) {
	failed := Exposure{Ready: true, Gaps: []string{"listening sockets"}}
	got := foldListen(Exposure{}, failed)
	if got.BaselineSeeded || len(got.BaselinePorts) != 0 {
		t.Fatalf("a failed socket read became the empty baseline: %+v", got)
	}
	prev := Exposure{
		Ready: true, BaselineSeeded: true, BaselineBindsSeeded: true,
		PublicPorts: []string{"22/tcp"}, ListeningOnAllInterfaces: []string{"22/tcp"},
		BaselinePorts: []string{"22/tcp"}, SpecificBinds: []string{"203.0.113.5 443/tcp"},
		BaselineBinds: []string{"203.0.113.5 443/tcp"},
	}
	got = foldListen(prev, failed)
	if !got.BaselineSeeded || len(got.PublicPorts) != 1 || got.PublicPorts[0] != "22/tcp" || len(got.SpecificBinds) != 1 {
		t.Fatalf("the last good socket list was dropped: %+v", got)
	}
	ok := foldListen(Exposure{}, Exposure{Ready: true, PublicPorts: []string{"22/tcp"}, SpecificBinds: []string{"203.0.113.5 443/tcp"}})
	if !ok.BaselineSeeded || len(ok.BaselinePorts) != 1 || !ok.BaselineBindsSeeded {
		t.Fatalf("a successful read was not kept: %+v", ok)
	}
	dockerFailed := foldListen(prev, Exposure{Ready: true, Gaps: []string{"docker ports"}, PublicPorts: []string{"80/tcp"}})
	if len(dockerFailed.PublicPorts) != 1 || dockerFailed.PublicPorts[0] != "22/tcp" || dockerFailed.BaselinePorts[0] != "22/tcp" {
		t.Fatalf("a failed Docker port read replaced the last list: %+v", dockerFailed)
	}
	firstDocker := foldListen(Exposure{}, Exposure{Ready: true, Gaps: []string{"docker ports"}, PublicPorts: []string{"80/tcp"}})
	if firstDocker.BaselineSeeded {
		t.Fatalf("the first partial read became the baseline: %+v", firstDocker)
	}
}
