package hostops

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

var unitNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._:\-]{0,200}\.service$`)

// protectedUnits cannot be stopped from esa. Losing any of them cuts the
// control plane off from the server or takes every container down.
var protectedUnits = map[string]bool{
	"ssh.service": true, "sshd.service": true, "docker.service": true, "containerd.service": true,
	"systemd-journald.service": true, "systemd-logind.service": true, "dbus.service": true,
	"systemd-networkd.service": true, "systemd-resolved.service": true, "NetworkManager.service": true,
}

// IsProtectedUnit reports whether esa refuses to stop the unit.
func IsProtectedUnit(unit string) bool { return protectedUnits[unit] }

// Services lists loaded service units.
func (h *Host) Services(ctx context.Context) ([]proto.Service, error) {
	out, err := h.output(ctx, 20*time.Second, "systemctl", "list-units", "--type=service", "--all", "--output=json", "--no-pager")
	if err != nil {
		return nil, err
	}
	var units []struct {
		Unit        string `json:"unit"`
		Load        string `json:"load"`
		Active      string `json:"active"`
		Sub         string `json:"sub"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(out), &units); err != nil {
		return nil, err
	}
	var svcs []proto.Service
	var names []string
	for _, u := range units {
		if u.Load != "loaded" || !unitNameRe.MatchString(u.Unit) {
			continue
		}
		svcs = append(svcs, proto.Service{
			Unit: u.Unit, Description: u.Description, Load: u.Load, Active: u.Active, Sub: u.Sub,
			Protected: protectedUnits[u.Unit],
		})
		names = append(names, u.Unit)
	}
	props := h.unitProps(ctx, names, "Id", "UnitFileState", "MemoryCurrent", "ActiveEnterTimestamp")
	for i := range svcs {
		p := props[svcs[i].Unit]
		svcs[i].Enabled = p["UnitFileState"]
		if n, err := strconv.ParseUint(p["MemoryCurrent"], 10, 64); err == nil {
			svcs[i].MemoryBytes = n
		}
		if svcs[i].Active == "active" {
			svcs[i].Since = parseSystemdTime(p["ActiveEnterTimestamp"])
		}
	}
	sort.Slice(svcs, func(i, j int) bool {
		fi, fj := svcs[i].Active == "failed", svcs[j].Active == "failed"
		if fi != fj {
			return fi
		}
		return svcs[i].Unit < svcs[j].Unit
	})
	return svcs, nil
}

// unitProps runs `systemctl show` in batches and returns properties by unit.
func (h *Host) unitProps(ctx context.Context, units []string, props ...string) map[string]map[string]string {
	out := map[string]map[string]string{}
	for start := 0; start < len(units); start += 100 {
		end := min(start+100, len(units))
		args := []string{"show", "--no-pager", "-p", strings.Join(props, ",")}
		args = append(args, units[start:end]...)
		raw, err := h.output(ctx, 20*time.Second, "systemctl", args...)
		if err != nil {
			continue
		}
		for _, block := range strings.Split(raw, "\n\n") {
			m := map[string]string{}
			for _, line := range strings.Split(block, "\n") {
				if k, v, ok := strings.Cut(line, "="); ok {
					m[k] = v
				}
			}
			if id := m["Id"]; id != "" {
				out[id] = m
			}
		}
	}
	return out
}

func parseSystemdTime(s string) time.Time {
	if s == "" || s == "n/a" {
		return time.Time{}
	}
	t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// ServiceAction starts, stops, or restarts a unit. Protected units can be
// restarted but never stopped.
func (h *Host) ServiceAction(ctx context.Context, unit, action string) error {
	if !unitNameRe.MatchString(unit) {
		return &BadRequestError{Reason: "invalid unit name"}
	}
	switch action {
	case "start", "restart":
	case "stop":
		if protectedUnits[unit] {
			return &RefusedError{Reason: unit + " is protected and cannot be stopped from esa"}
		}
	default:
		return &BadRequestError{Reason: "action must be start, stop, or restart"}
	}
	_, err := h.output(ctx, 2*time.Minute, "systemctl", action, "--no-block", unit)
	return err
}
