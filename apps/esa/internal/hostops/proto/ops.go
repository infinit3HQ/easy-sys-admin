package proto

import "time"

// ---- Updates -------------------------------------------------------------

// UpdateMode selects which packages an upgrade installs.
type UpdateMode string

const (
	UpdateSecurity UpdateMode = "security" // packages from *-security archives only
	UpdateAll      UpdateMode = "all"      // everything `apt-get upgrade` would install
)

// PendingUpdates answers `esa-helper updates`.
type PendingUpdates struct {
	Packages       []PendingPackage `json:"packages"`
	Security       int              `json:"security"`
	Other          int              `json:"other"`
	Held           []string         `json:"held"`
	ListsUpdatedAt time.Time        `json:"listsUpdatedAt"`
}

// PendingPackage is one upgradable package.
type PendingPackage struct {
	Name     string `json:"name"`
	From     string `json:"from"`
	To       string `json:"to"`
	Archive  string `json:"archive"`
	Security bool   `json:"security"`
}

// PackageVersion is one upgraded package.
type PackageVersion struct {
	Name   string `json:"name"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// Precheck answers `esa-helper upgrade-precheck`.
type Precheck struct {
	OK       bool     `json:"ok"`
	Problems []string `json:"problems"`
	Held     []string `json:"held"`
}

// UpgradeStarted answers `esa-helper upgrade-start`.
type UpgradeStarted struct {
	Unit string `json:"unit"`
}

// UpgradeResult is written by the detached upgrade job and read back with
// `esa-helper upgrade-status`.
type UpgradeResult struct {
	RunID      int64      `json:"runId"`
	State      string     `json:"state"` // running | succeeded | failed
	Mode       UpdateMode `json:"mode"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt time.Time  `json:"finishedAt,omitzero"`
	Packages   []string   `json:"packages"`
	Skipped    []string   `json:"skipped,omitempty"`
	// NotApproved are pending packages left out because the run's --allow
	// list did not name them.
	NotApproved []string `json:"notApproved,omitempty"`
	// Versions are the upgraded packages with the version before (from
	// apt's plan) and after (read from dpkg once the install finished).
	// After is empty when dpkg could not be read: unknown, not guessed.
	Versions       []PackageVersion `json:"versions,omitempty"`
	ExitCode       int              `json:"exitCode"`
	Error          string           `json:"error,omitempty"`
	RebootRequired bool             `json:"rebootRequired"`
	LogTail        []string         `json:"logTail,omitempty"`
}

// ---- Cleanups --------------------------------------------------------------

// Cleanup ids.
const (
	CleanBuildCache = "buildcache"
	CleanVSCode     = "vscode"
	CleanJournal    = "journal"
)

// CleanReport answers `esa-helper clean`.
type CleanReport struct {
	Applied bool          `json:"applied"`
	Items   []CleanResult `json:"items"`
}

// CleanResult is one cleanup on one server.
type CleanResult struct {
	ID string `json:"id"`
	// Status is measured | cleaned | skipped | failed.
	Status         string   `json:"status"`
	Reason         string   `json:"reason,omitempty"`
	ReclaimableNow int64    `json:"reclaimable"`
	Freed          int64    `json:"freed"`
	Removed        []string `json:"removed,omitempty"`
	Error          string   `json:"error,omitempty"`
}

// ---- Services --------------------------------------------------------------

// Service is one systemd service unit.
type Service struct {
	Unit        string    `json:"unit"`
	Description string    `json:"description"`
	Load        string    `json:"load"`
	Active      string    `json:"active"`
	Sub         string    `json:"sub"`
	Enabled     string    `json:"enabled"`
	MemoryBytes uint64    `json:"memoryBytes"`
	Since       time.Time `json:"since,omitzero"`
	Protected   bool      `json:"protected"`
}

// ---- Containers -------------------------------------------------------------

// Container is one Docker container.
type Container struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Image          string            `json:"image"`
	ImageID        string            `json:"imageId"`
	State          string            `json:"state"`
	Status         string            `json:"status"`
	Health         string            `json:"health,omitempty"`
	RestartCount   int               `json:"restartCount"`
	RestartPolicy  string            `json:"restartPolicy"`
	CreatedAt      time.Time         `json:"createdAt"`
	StartedAt      time.Time         `json:"startedAt,omitzero"`
	Ports          []string          `json:"ports"`
	ComposeProject string            `json:"composeProject,omitempty"`
	ComposeService string            `json:"composeService,omitempty"`
	Protected      bool              `json:"protected"`
	Labels         map[string]string `json:"-"`
}

// ImageCheck answers `esa-helper image-check`.
type ImageCheck struct {
	Container      string `json:"container"`
	Image          string `json:"image"`
	LocalDigest    string `json:"localDigest"`
	RemoteDigest   string `json:"remoteDigest"`
	UpdateAvailble bool   `json:"updateAvailable"`
	Error          string `json:"error,omitempty"`
}

// ---- Logs --------------------------------------------------------------------

// LogLine is one log entry.
type LogLine struct {
	Time     time.Time `json:"time"`
	Priority int       `json:"priority"` // syslog priority 0-7; 6 for container logs
	Message  string    `json:"message"`
	Stream   string    `json:"stream,omitempty"` // stdout | stderr for containers
}

// ---- Audits ------------------------------------------------------------------

// ReportRevision is set on access and security reports from a helper that
// collects the fields below. An older helper omits it. The control plane
// treats 0 as "reinstall the helper" and does not read the zero values as
// a clean bill of health.
const ReportRevision = 2

// AccessReport answers `esa-helper access`.
type AccessReport struct {
	Revision             int                 `json:"revision"`
	Users                []LocalUser         `json:"users"`
	Groups               map[string][]string `json:"groups"`
	Logins               []Login             `json:"logins"`
	FailedSSH24h         int                 `json:"failedSsh24h"`
	PasswordAuth         bool                `json:"passwordAuth"`
	PermitRootLogin      string              `json:"permitRootLogin"`
	PermitEmptyPasswords bool                `json:"permitEmptyPasswords"`
	PubkeyAuth           bool                `json:"pubkeyAuth"`
	X11Forwarding        bool                `json:"x11Forwarding"`
	AllowTcpForwarding   string              `json:"allowTcpForwarding"`
	KbdInteractive       bool                `json:"kbdInteractive"`
	ListenPorts          []int               `json:"listenPorts"`
	ListenAddresses      []string            `json:"listenAddresses"`
	// SSHRead is true when `sshd -T` returned a config. The bools above are
	// meaningful only then.
	SSHRead        bool       `json:"sshRead"`
	SudoRules      []SudoRule `json:"sudoRules"`
	SudoIncomplete bool       `json:"sudoIncomplete"`
	// Gaps names checks that did not run. Missing ssh settings are not "off".
	Gaps []string `json:"gaps"`
}

// SudoRule is one command spec from sudoers. Aliases are not expanded.
// Includes outside /etc/sudoers.d are not followed.
type SudoRule struct {
	Source       string `json:"source"`
	Who          string `json:"who"`
	RunAs        string `json:"runAs"`
	NoPassword   bool   `json:"noPassword"`
	Commands     string `json:"commands"`
	Unrestricted bool   `json:"unrestricted"`
}

// LocalUser is an account with a login shell.
// Password is locked, none, set, or unknown. It is never a hash.
type LocalUser struct {
	Name           string          `json:"name"`
	UID            int             `json:"uid"`
	Home           string          `json:"home"`
	Shell          string          `json:"shell"`
	Password       string          `json:"password"`
	AuthorizedKeys []AuthorizedKey `json:"authorizedKeys"`
}

// AuthorizedKey is one line of an authorized_keys file.
// From is the from= restriction. HasCommand is true when the line forces a
// command. The command text is not returned, because it can hold a secret.
type AuthorizedKey struct {
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Comment     string `json:"comment"`
	From        string `json:"from"`
	HasCommand  bool   `json:"hasCommand"`
}

// Login is one entry from `last`.
type Login struct {
	User string    `json:"user"`
	From string    `json:"from"`
	At   time.Time `json:"at"`
}

// SecurityReport answers `esa-helper security`.
type SecurityReport struct {
	Revision      int        `json:"revision"`
	Listening     []Listener `json:"listening"`
	Firewall      string     `json:"firewall"` // "ufw active", "nftables rules", "none"
	FirewallRules []string   `json:"firewallRules"`
	// FirewallDefaultIncoming is ufw's default incoming policy: deny,
	// reject, or allow. Empty when ufw is not active or the policy was not
	// read. With allow, a port no rule mentions is open.
	FirewallDefaultIncoming string    `json:"firewallDefaultIncoming,omitempty"`
	CrowdSec                string    `json:"crowdsec"` // active | inactive | not installed
	CrowdSecBans            int       `json:"crowdSecBans"`
	Fail2ban                string    `json:"fail2ban"`
	Fail2banJails           []BanJail `json:"fail2banJails"`
	UnattendedUpgrades      bool      `json:"unattendedUpgrades"`
	// Gaps names checks that did not run. An empty listener list is not a
	// clean result when "listening sockets" is listed here.
	Gaps []string `json:"gaps"`
}

// BanJail is one fail2ban jail and how many addresses it currently bans.
// The addresses themselves are not collected.
type BanJail struct {
	Name   string `json:"name"`
	Banned int    `json:"banned"`
}

// Listener is one listening socket.
// Exposure is all (every interface), local (loopback), or other (one address).
// Container is set when the process is docker-proxy and a container publishes the port.
type Listener struct {
	Proto     string `json:"proto"`
	Address   string `json:"address"`
	Port      int    `json:"port"`
	Process   string `json:"process"`
	User      string `json:"user"`
	Exposure  string `json:"exposure"`
	Container string `json:"container,omitempty"`
	// UFW is allow, deny, or unmentioned when ufw is active. Empty otherwise.
	// It is unknown for a port Docker publishes, because Docker forwards it
	// past ufw's input rules, whether or not ufw is active.
	UFW string `json:"ufw"`
}

// JobsReport answers `esa-helper jobs`.
type JobsReport struct {
	Cron   []CronEntry `json:"cron"`
	Timers []Timer     `json:"timers"`
}

// CronEntry is one cron line.
type CronEntry struct {
	Source   string `json:"source"`
	User     string `json:"user"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
}

// Timer is one systemd timer.
type Timer struct {
	Unit      string    `json:"unit"`
	Activates string    `json:"activates"`
	Next      time.Time `json:"next,omitzero"`
	Last      time.Time `json:"last,omitzero"`
	Result    string    `json:"result"`
	Failed    bool      `json:"failed"`
}

// RecoveryReport is what an operator wrote under /var/lib/esa/recovery.
// It is not a restore test run by esa.
type RecoveryReport struct {
	Revision  int              `json:"revision"`
	Statement string           `json:"statement"`
	Sources   []RecoverySource `json:"sources"`
	Gaps      []string         `json:"gaps"`
}

// RecoverySource is one restic, Borg, or generic result file.
type RecoverySource struct {
	Tool         string     `json:"tool"`
	ToolVersion  string     `json:"toolVersion"`
	Dataset      string     `json:"dataset"`
	Snapshot     string     `json:"snapshot"`
	BackupState  string     `json:"backupState"`
	BackupAt     *time.Time `json:"backupAt,omitempty"`
	CheckKind    string     `json:"checkKind"`
	CheckState   string     `json:"checkState"`
	CheckAt      *time.Time `json:"checkAt,omitempty"`
	RestoreState string     `json:"restoreState"`
	SmokeState   string     `json:"smokeState"`
	// Freshness is current, stale, unknown, or future. Stale means the
	// backup time is more than 7 days ago. Unknown is not a current success.
	Freshness string `json:"freshness"`
	Note      string `json:"note"`
}

// PackageInventory is the deb packages dpkg reports. It is not a vulnerability scan.
type PackageInventory struct {
	Distro   string             `json:"distro"`
	Release  string             `json:"release"`
	Packages []InstalledPackage `json:"packages"`
	Gaps     []string           `json:"gaps"`
}

// InstalledPackage is one installed deb.
type InstalledPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Arch    string `json:"arch"`
	// Source and SourceVersion are the source package it was built from.
	// Debian and Ubuntu advisories are mostly keyed by source package.
	// Empty from helpers older than this field.
	Source        string `json:"source,omitempty"`
	SourceVersion string `json:"sourceVersion,omitempty"`
}
