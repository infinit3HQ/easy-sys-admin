package fleet

import (
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
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
