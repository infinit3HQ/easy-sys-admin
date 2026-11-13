package store

import (
	"time"
)

// Run is one maintenance job across one or more servers.
type Run struct {
	ID        int64  `gorm:"primaryKey"`
	AccountID int64  `gorm:"not null;index:runs_account_started,priority:1"`
	GroupID   *int64 `gorm:"index"`
	// Kind is measure | clean | update | reboot.
	Kind string `gorm:"not null;check:runs_kind_check,kind IN ('measure','clean','update','reboot')"`
	// Trigger is schedule | manual | api | mcp.
	Trigger string `gorm:"not null"`
	// Status is running | succeeded | failed | partial | missed.
	Status      string `gorm:"not null;default:'running'"`
	Options     JSON   `gorm:"type:jsonb;not null;default:'{}'"`
	RequestedBy *int64
	StartedAt   time.Time `gorm:"not null;index:runs_account_started,priority:2,sort:desc"`
	FinishedAt  *time.Time
	Summary     string `gorm:"not null;default:''"`
	// GateState is the canary gate of a run that waits for a person after
	// its canaries: waiting, continued, stopped, or expired. Empty when
	// the run has no gate or has not reached it.
	GateState string `gorm:"not null;default:''"`
	GateAt    *time.Time
	GateBy    *int64
	GateNote  string `gorm:"not null;default:''"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
	Group   *Group  `gorm:"constraint:OnDelete:SET NULL"`
	Servers []RunServer
}

// RunServer is one server's part of a run.
type RunServer struct {
	ID       int64 `gorm:"primaryKey"`
	RunID    int64 `gorm:"not null;index"`
	ServerID int64 `gorm:"not null;index"`
	// Status is queued | running | succeeded | failed | skipped.
	Status     string `gorm:"not null;default:'queued'"`
	Detail     JSON   `gorm:"type:jsonb;not null;default:'{}'"`
	Error      string `gorm:"not null;default:''"`
	BytesFreed int64  `gorm:"not null;default:0"`
	StartedAt  *time.Time
	FinishedAt *time.Time

	Run    Run    `gorm:"constraint:OnDelete:CASCADE"`
	Server Server `gorm:"constraint:OnDelete:CASCADE"`
}

// AlertRule is one alert condition for an account.
type AlertRule struct {
	ID        int64 `gorm:"primaryKey"`
	AccountID int64 `gorm:"not null;uniqueIndex:alert_rules_account_kind_key,priority:1"`
	// Kind is disk | inode | memory | unreachable | failed_unit | container | reboot_required | cert_expiry | run_failed.
	Kind      string  `gorm:"not null;uniqueIndex:alert_rules_account_kind_key,priority:2"`
	Enabled   bool    `gorm:"not null;default:true"`
	Threshold float64 `gorm:"not null;default:0"`
	// ForMinutes is how long the condition must hold before it fires.
	ForMinutes int       `gorm:"not null;default:0"`
	CreatedAt  time.Time `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
}

// Alert is one firing (or resolved) condition. Subject identifies what it
// is about inside the server, for example a mount point or unit name.
type Alert struct {
	ID           int64     `gorm:"primaryKey"`
	AccountID    int64     `gorm:"not null;index:alerts_account_open,priority:1"`
	RuleID       int64     `gorm:"not null;index"`
	ServerID     *int64    `gorm:"index"`
	Kind         string    `gorm:"not null"`
	Subject      string    `gorm:"not null;default:''"`
	Severity     string    `gorm:"not null;default:'warning'"`
	Message      string    `gorm:"not null"`
	PendingSince time.Time `gorm:"not null"`
	FiredAt      *time.Time
	ResolvedAt   *time.Time `gorm:"index:alerts_account_open,priority:2"`
	Notified     bool       `gorm:"not null;default:false"`

	Account Account   `gorm:"constraint:OnDelete:CASCADE"`
	Rule    AlertRule `gorm:"constraint:OnDelete:CASCADE"`
	Server  *Server   `gorm:"constraint:OnDelete:CASCADE"`
}

// SecurityIncident is one reviewable security notice. Acknowledgement means
// a person has seen it. It does not mean the condition was fixed, and it
// does not change the server.
type SecurityIncident struct {
	ID        int64  `gorm:"primaryKey"`
	AccountID int64  `gorm:"not null;uniqueIndex:security_incidents_key,priority:1"`
	ServerID  int64  `gorm:"not null;uniqueIndex:security_incidents_key,priority:2"`
	Kind      string `gorm:"not null;size:40;uniqueIndex:security_incidents_key,priority:3"`
	Subject   string `gorm:"not null;size:200;uniqueIndex:security_incidents_key,priority:4"`
	// State is open, acknowledged, snoozed, accepted (a risk accepted until
	// SnoozeUntil, with SnoozeReason), or resolved (a fresh check no longer
	// saw it). Missing data never resolves a notice.
	State     string    `gorm:"not null;default:'open'"`
	Message   string    `gorm:"not null"`
	FirstSeen time.Time `gorm:"not null"`
	LastSeen  time.Time `gorm:"not null"`
	AckedBy   *int64
	AckedAt   *time.Time
	// SnoozeUntil is set while State is snoozed. After it passes, the notice
	// returns to open.
	SnoozeUntil  *time.Time
	SnoozeReason string `gorm:"not null;default:''"`
	// ResolvedAt is when a fresh check stopped seeing it. Recurrences counts
	// how often it came back after that.
	ResolvedAt  *time.Time
	Recurrences int `gorm:"not null;default:0"`
	// Note is the reviewer's latest note.
	Note string `gorm:"not null;default:''"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
	Server  Server  `gorm:"constraint:OnDelete:CASCADE"`
}

// IncidentEvent is one step in a notice's history: opened, reopened,
// acknowledged, snoozed, accepted, resolved, or returned after a snooze.
type IncidentEvent struct {
	ID         int64     `gorm:"primaryKey"`
	IncidentID int64     `gorm:"not null;index"`
	Kind       string    `gorm:"not null;size:40"`
	ActorID    *int64    // nil for the control plane itself
	Detail     string    `gorm:"not null;default:''"`
	At         time.Time `gorm:"not null"`

	Incident SecurityIncident `gorm:"constraint:OnDelete:CASCADE"`
}

// NotifyDelivery is one queued notification. It is written before the
// in-memory workers see it, so a restart does not drop it.
type NotifyDelivery struct {
	ID        int64  `gorm:"primaryKey"`
	AccountID int64  `gorm:"not null;index"`
	ChannelID int64  `gorm:"not null;index"`
	Title     string `gorm:"not null"`
	Body      string `gorm:"not null"`
	Severity  string `gorm:"not null"`
	URL       string `gorm:"not null;default:''"`
	// State is queued, sending, sent, or dead.
	State     string `gorm:"not null;default:'queued';index"`
	Attempts  int    `gorm:"not null;default:0"`
	LastError string `gorm:"not null;default:''"`
	CreatedAt time.Time
	// ClaimedAt is when the current send started. A stale send is judged
	// on it, not on CreatedAt.
	ClaimedAt *time.Time
	SentAt    *time.Time

	Account Account       `gorm:"constraint:OnDelete:CASCADE"`
	Channel NotifyChannel `gorm:"constraint:OnDelete:CASCADE"`
}

// ScanImport is one vulnerability scan result an operator imported for a
// server. esa does not run the scanner or download its database; the
// result is evidence from that tool, with its version and database age.
type ScanImport struct {
	ID             int64  `gorm:"primaryKey"`
	AccountID      int64  `gorm:"not null;index"`
	ServerID       int64  `gorm:"not null;index"`
	Scanner        string `gorm:"not null;size:40"`
	ScannerVersion string `gorm:"not null;default:'';size:40"`
	DBBuilt        *time.Time
	Matches        JSON      `gorm:"type:jsonb;not null"`
	ImportedAt     time.Time `gorm:"not null"`
	ImportedBy     int64     `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
	Server  Server  `gorm:"constraint:OnDelete:CASCADE"`
}

// HeldWarning is a warning waiting for the space's daily summary. It is
// stored, so a restart does not lose it, and deleted once summarised.
type HeldWarning struct {
	ID        int64     `gorm:"primaryKey"`
	AccountID int64     `gorm:"not null;index"`
	Title     string    `gorm:"not null"`
	URL       string    `gorm:"not null;default:''"`
	CreatedAt time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
}

// NotifyChannel is where alerts are sent. ConfigEnc holds the URL, token,
// or SMTP settings, sealed with crypto.Box.
type NotifyChannel struct {
	ID        int64  `gorm:"primaryKey"`
	AccountID int64  `gorm:"not null;index"`
	Name      string `gorm:"not null;size:100"`
	// Kind is ntfy | webhook | discord | slack | email.
	Kind       string `gorm:"not null;check:notify_channels_kind_check,kind IN ('ntfy','webhook','discord','slack','email')"`
	ConfigEnc  string `gorm:"not null"`
	Enabled    bool   `gorm:"not null;default:true"`
	LastError  string `gorm:"not null;default:''"`
	LastSentAt *time.Time
	CreatedAt  time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
}

// CertTarget is a TLS endpoint whose certificate expiry is checked daily.
type CertTarget struct {
	ID        int64  `gorm:"primaryKey"`
	AccountID int64  `gorm:"not null;uniqueIndex:cert_targets_account_host_key,priority:1"`
	Hostname  string `gorm:"not null;uniqueIndex:cert_targets_account_host_key,priority:2"`
	Port      int    `gorm:"not null;default:443;uniqueIndex:cert_targets_account_host_key,priority:3"`
	ExpiresAt *time.Time
	Issuer    string `gorm:"not null;default:''"`
	LastError string `gorm:"not null;default:''"`
	CheckedAt *time.Time
	CreatedAt time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
}

// FixPlan is a typed maintenance request. A token can propose or execute it.
// Only a browser session can approve it. The body is the authority; prose is not stored.
type FixPlan struct {
	ID              int64  `gorm:"primaryKey"`
	AccountID       int64  `gorm:"not null;index"`
	Status          string `gorm:"not null"`
	Fingerprint     string `gorm:"not null;size:64"`
	Body            JSON   `gorm:"type:jsonb;not null"`
	ExpiresAt       *time.Time
	CreatedBy       int64 `gorm:"not null"`
	ProposerTokenID int64 `gorm:"not null;default:0"`
	ApprovedBy      *int64
	ApprovedAt      *time.Time
	Reason          string `gorm:"not null;default:''"`
	RunID           *int64
	CreatedAt       time.Time `gorm:"not null"`
	UpdatedAt       time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
}

// APIToken authenticates the CLI and MCP clients. Only the SHA-256 of the
// token is stored; Prefix is the first characters, shown in the UI.
type APIToken struct {
	ID         int64  `gorm:"primaryKey"`
	UserID     int64  `gorm:"not null;index"`
	AccountID  int64  `gorm:"not null"`
	Name       string `gorm:"not null;size:100"`
	Scopes     JSON   `gorm:"type:jsonb;not null;default:'[]'"`
	TokenHash  []byte `gorm:"not null;uniqueIndex"`
	Prefix     string `gorm:"not null"`
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time `gorm:"not null"`

	User    User    `gorm:"constraint:OnDelete:CASCADE"`
	Account Account `gorm:"constraint:OnDelete:CASCADE"`
}

// TableName avoids GORM's "a_p_i_tokens".
func (APIToken) TableName() string { return "api_tokens" }
