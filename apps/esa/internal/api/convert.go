package api

import (
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// Conversions from fleet types to generated API types. Keeping them here
// means the fleet package never depends on the wire format.

func toMembership(m store.Membership) AccountMembership {
	return AccountMembership{Id: m.Account.ID, Name: m.Account.Name, Role: Role(m.Role)}
}

func toMe(me fleet.Me) Me {
	accounts := make([]AccountMembership, 0, len(me.Accounts))
	for _, m := range me.Accounts {
		accounts = append(accounts, toMembership(m))
	}
	return Me{
		User:     User{Id: me.User.ID, Email: me.User.Email, Name: me.User.Name},
		Account:  toMembership(me.Current),
		Accounts: accounts,
	}
}

func toOverview(ov fleet.Overview) FleetOverview {
	servers := make([]ServerSummary, 0, len(ov.Servers))
	for _, s := range ov.Servers {
		row := ServerSummary{Id: s.ID, Name: s.Name, Host: s.Host, Status: ServerStatus(s.Status)}
		if s.GroupName != "" {
			row.GroupName = &s.GroupName
		}
		if !s.LastSeenAt.IsZero() {
			row.LastSeenAt = &s.LastSeenAt
		}
		servers = append(servers, row)
	}
	t := ov.Totals
	return FleetOverview{
		Totals: FleetTotals{
			Servers: t.Servers, Groups: t.Groups, Ok: t.OK,
			Warning: t.Warning, Failing: t.Failing, Unreachable: t.Unreachable,
		},
		Servers: servers,
	}
}
