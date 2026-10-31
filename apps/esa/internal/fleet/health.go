package fleet

import (
	"fmt"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// ServerStatus is the health of one server as shown on the overview.
type ServerStatus string

const (
	StatusOK          ServerStatus = "ok"
	StatusWarning     ServerStatus = "warning"
	StatusFailing     ServerStatus = "failing"
	StatusUnreachable ServerStatus = "unreachable"
	StatusPending     ServerStatus = "pending"
)

// unreachableAfter is how many polls in a row may fail before a server is unreachable.
const unreachableAfter = 3

// Thresholds for the status dot. Alert rules have their own, editable thresholds.
const (
	diskWarn, diskFail   = 85.0, 95.0
	memWarn, memFail     = 90.0, 97.0
	inodeWarn, inodeFail = 85.0, 95.0
)

// evaluate turns a snapshot into a status and the reasons for it.
func evaluate(st *proto.Status, upd *proto.PendingUpdates) (ServerStatus, []string) {
	status := StatusOK
	reasons := []string{}
	raise := func(to ServerStatus, why string) {
		reasons = append(reasons, why)
		if to == StatusFailing || status == StatusOK {
			status = to
		}
	}
	for _, d := range st.Disks {
		if p := pct(d.Used, d.Total); p >= diskFail {
			raise(StatusFailing, fmt.Sprintf("disk %s at %.0f%%", d.Mount, p))
		} else if p >= diskWarn {
			raise(StatusWarning, fmt.Sprintf("disk %s at %.0f%%", d.Mount, p))
		}
		if p := pct(d.InodesUsed, d.InodesTotal); p >= inodeFail {
			raise(StatusFailing, fmt.Sprintf("inodes on %s at %.0f%%", d.Mount, p))
		} else if p >= inodeWarn {
			raise(StatusWarning, fmt.Sprintf("inodes on %s at %.0f%%", d.Mount, p))
		}
	}
	if p := pct(st.MemUsed, st.MemTotal); p >= memFail {
		raise(StatusFailing, fmt.Sprintf("memory at %.0f%%", p))
	} else if p >= memWarn {
		raise(StatusWarning, fmt.Sprintf("memory at %.0f%%", p))
	}
	if n := len(st.FailedUnits); n > 0 {
		raise(StatusWarning, plural(n, "failed unit", "failed units"))
	}
	if st.Docker != nil {
		if bad := st.Docker.Unhealthy + st.Docker.Restarting; bad > 0 {
			raise(StatusWarning, plural(bad, "unhealthy container", "unhealthy containers"))
		}
	}
	if st.RebootRequired {
		raise(StatusWarning, "reboot required")
	}
	if upd != nil && upd.Security > 0 {
		raise(StatusWarning, plural(upd.Security, "security update", "security updates"))
	}
	if !st.TimeSynced {
		raise(StatusWarning, "clock not synchronised")
	}
	return status, reasons
}

func pct(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(used) / float64(total)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// worstDisk returns the fullest real filesystem's use in percent.
func worstDisk(st *proto.Status) float64 {
	worst := 0.0
	for _, d := range st.Disks {
		if p := pct(d.Used, d.Total); p > worst {
			worst = p
		}
	}
	return worst
}
