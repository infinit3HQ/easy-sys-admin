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

// Features are behaviours of existing actions that the control plane must
// know about before relying on them. They are reported with the actions.
var Features = []string{
	// FeatureUpgradeAllow: upgrade-start and upgrade-exec take --allow and
	// install only those packages, refusing anything apt would add.
	FeatureUpgradeAllow,
}

// FeatureUpgradeAllow is the --allow flag of upgrade-start.
const FeatureUpgradeAllow = "upgrade-allow"

// AllowNone is the --allow value for an empty list. It is not a valid
// package name, so it cannot be confused with one.
const AllowNone = "-"

// Capabilities is what this helper build reports: its actions and features.
func Capabilities() []string {
	return append(append([]string{}, Actions...), Features...)
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
	for _, a := range Capabilities() {
		if !Supports(capabilities, a) {
			out = append(out, a)
		}
	}
	return out
}
