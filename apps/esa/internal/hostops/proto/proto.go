// Package proto defines the JSON messages between the control plane and
// esa-helper. It has no dependencies, so both sides import it.
//
// Every helper invocation prints exactly one Envelope on stdout.
package proto

import (
	"encoding/json"
	"time"
)

// Version is the helper protocol version. The control plane refuses to talk
// to a helper with a different major protocol and offers to update it.
const Version = 1

// HelperPath is where the helper is installed on managed servers.
const HelperPath = "/usr/local/lib/esa/esa-helper"

// StateDir holds the helper's own files on the server (upgrade results).
const StateDir = "/var/lib/esa"

// UserHome and UserComment mark the esa user as ours. Install reuses an
// existing esa user, and uninstall deletes it, only when both match.
const (
	UserHome    = "/var/lib/esa-home"
	UserComment = "easy-sys-admin control plane"
)

// Envelope wraps every helper response.
type Envelope struct {
	OK       bool            `json:"ok"`
	Protocol int             `json:"protocol"`
	Data     json.RawMessage `json:"data,omitempty"`
	Error    string          `json:"error,omitempty"`
	// Code classifies errors the control plane reacts to, for example "refused".
	Code string `json:"code,omitempty"`
}

// Error codes.
const (
	CodeRefused    = "refused"     // a safety rule blocked the action
	CodeBadRequest = "bad_request" // invalid arguments
	CodeFailed     = "failed"      // the action ran and failed
	CodeBusy       = "busy"        // another operation holds a lock (apt, dpkg)
	// CodeIncompatible is set by the control plane, not the helper: the
	// installed helper does not have the action, so it was never sent.
	CodeIncompatible = "helper_incompatible"
)

// VersionInfo answers `esa-helper version`.
type VersionInfo struct {
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	Protocol int    `json:"protocol"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	// Capabilities lists every action this helper build has. A helper
	// older than this field sends none; see LegacyActions.
	Capabilities []string `json:"capabilities,omitempty"`
}

// Status is the snapshot the poller collects every minute.
type Status struct {
	CollectedAt   time.Time `json:"collectedAt"`
	Hostname      string    `json:"hostname"`
	OS            string    `json:"os"`
	Kernel        string    `json:"kernel"`
	Arch          string    `json:"arch"`
	BootTime      time.Time `json:"bootTime"`
	UptimeSeconds int64     `json:"uptimeSeconds"`

	CPUCount   int     `json:"cpuCount"`
	CPUPercent float64 `json:"cpuPercent"`
	Load1      float64 `json:"load1"`
	Load5      float64 `json:"load5"`
	Load15     float64 `json:"load15"`

	MemTotal     uint64 `json:"memTotal"`
	MemUsed      uint64 `json:"memUsed"`
	MemAvailable uint64 `json:"memAvailable"`
	SwapTotal    uint64 `json:"swapTotal"`
	SwapUsed     uint64 `json:"swapUsed"`

	Disks []Disk  `json:"disks"`
	Net   []NetIf `json:"net"`

	TimeSynced     bool     `json:"timeSynced"`
	RebootRequired bool     `json:"rebootRequired"`
	RebootPackages []string `json:"rebootPackages,omitempty"`
	FailedUnits    []string `json:"failedUnits"`

	Docker *DockerSummary `json:"docker,omitempty"`
}

// Disk is one mounted filesystem.
type Disk struct {
	Mount       string `json:"mount"`
	Device      string `json:"device"`
	FSType      string `json:"fsType"`
	Total       uint64 `json:"total"`
	Used        uint64 `json:"used"`
	Avail       uint64 `json:"avail"`
	InodesTotal uint64 `json:"inodesTotal"`
	InodesUsed  uint64 `json:"inodesUsed"`
}

// NetIf is cumulative traffic on one interface since boot.
type NetIf struct {
	Name    string `json:"name"`
	RxBytes uint64 `json:"rxBytes"`
	TxBytes uint64 `json:"txBytes"`
}

// DockerSummary is container counts and Docker disk use.
type DockerSummary struct {
	Version    string `json:"version"`
	Running    int    `json:"running"`
	Exited     int    `json:"exited"`
	Restarting int    `json:"restarting"`
	Unhealthy  int    `json:"unhealthy"`
	Total      int    `json:"total"`

	ImagesBytes     int64 `json:"imagesBytes"`
	BuildCacheBytes int64 `json:"buildCacheBytes"`
	VolumesBytes    int64 `json:"volumesBytes"`
	ContainersBytes int64 `json:"containersBytes"`
}
