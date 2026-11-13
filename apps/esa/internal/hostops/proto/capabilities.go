package proto

import "slices"

// Actions is every action this helper build answers. The helper reports it
// from `esa-helper version`, and the control plane sends an action only
// when the installed helper listed it. Add a new action here in the same
// change that adds it to cmd/esa-helper.
var Actions = []string{
	"version",
	"status", "updates", "upgrade-precheck", "upgrade-start", "upgrade-exec", "upgrade-status",
	"clean", "services", "service", "containers", "container", "container-update", "image-check",
	"logs", "access", "security", "jobs", "recovery", "inventory", "reboot", "uninstall",
}

// LegacyActions is what a protocol 1 helper had before helpers reported
// their capabilities. A helper that sends no list is assumed to have these
// and nothing newer, so a new action is never sent to an old helper.
var LegacyActions = []string{
	"version",
	"status", "updates", "upgrade-precheck", "upgrade-start", "upgrade-exec", "upgrade-status",
	"clean", "services", "service", "containers", "container", "container-update", "image-check",
	"logs", "access", "security", "jobs", "reboot", "uninstall",
}

// Supports reports whether a helper with these capabilities has action.
// nil capabilities mean the helper did not report any: LegacyActions.
// "version" is always allowed, so the control plane can ask.
func Supports(capabilities []string, action string) bool {
	if action == "version" {
		return true
	}
	if capabilities == nil {
		return slices.Contains(LegacyActions, action)
	}
	return slices.Contains(capabilities, action)
}

// Missing lists the actions in Actions that a helper with these
// capabilities lacks: what a reinstall would add.
func Missing(capabilities []string) []string {
	out := []string{}
	for _, a := range Actions {
		if !Supports(capabilities, a) {
			out = append(out, a)
		}
	}
	return out
}
