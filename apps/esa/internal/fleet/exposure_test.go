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
		},
	}
	ex := summarizeExposure(access, sec, at)
	if !ex.Ready || !ex.PasswordAuth || ex.Firewall != "none" || ex.ExtraRoot != 1 || ex.NoPasswordAll != 1 {
		t.Fatalf("summary: %+v", ex)
	}
	if len(ex.PublicPorts) != 2 || ex.PublicPorts[0] != "22/tcp" || ex.PublicPorts[1] != "443/tcp" {
		t.Fatalf("ports: %+v", ex.PublicPorts)
	}
	old := summarizeExposure(proto.AccessReport{}, proto.SecurityReport{Listening: []proto.Listener{{Exposure: "all", Port: 22, Proto: "tcp"}}}, at)
	if old.Ready || len(old.PublicPorts) != 0 || old.PasswordAuth {
		t.Fatalf("an old helper must not look clean: %+v", old)
	}
	if !exposureChanged(ex, Exposure{Ready: true, Firewall: "ufw active", PublicPorts: []string{}}) {
		t.Fatal("a firewall and port change must be recorded")
	}
	if exposureChanged(Exposure{}, ex) {
		t.Fatal("the first summary is not a change")
	}
	conds := exposureConditions(ex)
	if len(conds) != 4 {
		t.Fatalf("conditions: %+v", conds)
	}
}
