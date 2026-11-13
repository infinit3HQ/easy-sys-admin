package store

import (
	"time"
)

// AccountKey is the SSH keypair the control plane uses to log in to an
// account's servers as the esa user. PrivateKeyEnc is sealed with crypto.Box.
type AccountKey struct {
	AccountID     int64     `gorm:"primaryKey;autoIncrement:false"`
	PublicKey     string    `gorm:"not null"` // authorized_keys format
	Fingerprint   string    `gorm:"not null"`
	PrivateKeyEnc string    `gorm:"not null"`
	CreatedAt     time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
}

// Group is a set of servers that share a maintenance policy.
type Group struct {
	ID        int64  `gorm:"primaryKey"`
	AccountID int64  `gorm:"not null;uniqueIndex:groups_account_name_key,priority:1"`
	Name      string `gorm:"not null;size:100;uniqueIndex:groups_account_name_key,priority:2"`
	Timezone  string `gorm:"not null;default:'UTC'"`

	// Cleanup schedule.
	CleanEnabled    bool   `gorm:"not null;default:false"`
	CleanDays       string `gorm:"not null;default:'mon,tue,wed,thu,fri,sat,sun'"` // comma list
	CleanTime       string `gorm:"not null;default:'03:30'"`                       // HH:MM in Timezone
	CleanBuildCache bool   `gorm:"not null;default:true"`
	CleanVSCode     bool   `gorm:"not null;default:true"`
	CleanJournal    bool   `gorm:"not null;default:false"`
	JournalSize     string `gorm:"not null;default:'200M'"`

	// Update schedule.
	UpdatesEnabled   bool   `gorm:"not null;default:false"`
	UpdateMode       string `gorm:"not null;default:'security';check:groups_update_mode_check,update_mode IN ('security','all')"`
	UpdateDays       string `gorm:"not null;default:'sun'"`
	UpdateTime       string `gorm:"not null;default:'04:00'"`
	HoldPackages     JSON   `gorm:"type:jsonb;not null;default:'[]'"`
	RebootIfRequired bool   `gorm:"not null;default:false"`
	// Rolling updates: servers worked on at once, and failed servers that
	// stop the run. Reboot runs use them too.
	UpdateBatchSize   int `gorm:"not null;default:1"`
	UpdateMaxFailures int `gorm:"not null;default:1"`
	// HealthChecks run before and after a package upgrade. A blocking
	// failure stops that server. The list is frozen onto the run.
	HealthChecks JSON `gorm:"type:jsonb;not null;default:'[]'"`
	// PlanRequired makes an interactive update or cleanup wait for an
	// approved plan. A schedule still runs.
	PlanRequired bool `gorm:"not null;default:false"`

	// Last schedule slot each job claimed, so a slot runs once.
	CleanSlotAt  *time.Time
	UpdateSlotAt *time.Time

	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
}

// Server is one managed machine.
type Server struct {
	ID        int64  `gorm:"primaryKey"`
	AccountID int64  `gorm:"not null;index;uniqueIndex:servers_account_name_key,priority:1"`
	GroupID   *int64 `gorm:"index"`
	Name      string `gorm:"not null;size:100;uniqueIndex:servers_account_name_key,priority:2"`
	Host      string `gorm:"not null"`
	Port      int    `gorm:"not null;default:22"`
	// Username is the SSH user for normal operation, "esa" once installed.
	Username string `gorm:"not null;default:'esa'"`

	// Enrollment: pending -> ready. "Pending" servers have no accepted host key yet.
	Enrollment string `gorm:"not null;default:'pending';check:servers_enrollment_check,enrollment IN ('pending','ready')"`

	HostKey            string `gorm:"not null;default:''"` // authorized_keys format
	HostKeyFingerprint string `gorm:"not null;default:''"`
	HostKeyAcceptedBy  *int64
	HostKeyAcceptedAt  *time.Time

	HelperVersion string `gorm:"not null;default:''"`
	// HelperProtocol and HelperCapabilities are what `esa-helper version`
	// reported at HelperCheckedAt. Null capabilities mean an older helper
	// that does not list them; nil HelperCheckedAt means never asked.
	HelperProtocol     int  `gorm:"not null;default:0"`
	HelperCapabilities JSON `gorm:"type:jsonb"`
	HelperCheckedAt    *time.Time
	Arch               string `gorm:"not null;default:''"`

	// HostsControlPlane marks the server esa itself runs on. It is never
	// rebooted by a schedule.
	HostsControlPlane bool `gorm:"not null;default:false"`
	// ProtectedContainers are name prefixes; empty means the defaults.
	ProtectedContainers JSON `gorm:"type:jsonb;not null;default:'[]'"`
	// Note is a short reminder kept in the control plane. It is never sent to the server.
	Note string `gorm:"not null;default:''"`

	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
	Group   *Group  `gorm:"constraint:OnDelete:SET NULL"`
}

// KeyOccurrence is one authorized key seen on one login of one server.
// It is only what the helper read from the standard authorized_keys files.
type KeyOccurrence struct {
	ID          int64     `gorm:"primaryKey"`
	AccountID   int64     `gorm:"not null;index"`
	ServerID    int64     `gorm:"not null;uniqueIndex:key_occurrences_place,priority:1"`
	User        string    `gorm:"not null;size:64;uniqueIndex:key_occurrences_place,priority:2"`
	Fingerprint string    `gorm:"not null;size:100;uniqueIndex:key_occurrences_place,priority:3"`
	KeyType     string    `gorm:"not null;default:''"`
	Comment     string    `gorm:"not null;default:''"`
	From        string    `gorm:"not null;default:''"`
	HasCommand  bool      `gorm:"not null;default:false"`
	ObservedAt  time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
	Server  Server  `gorm:"constraint:OnDelete:CASCADE"`
}

// KeyLabel is the name an operator gives a fingerprint in one space.
type KeyLabel struct {
	AccountID   int64     `gorm:"primaryKey"`
	Fingerprint string    `gorm:"primaryKey;size:100"`
	Label       string    `gorm:"not null;size:80"`
	UpdatedAt   time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
}

// AccessIdentity is who a key label stands for: a person, a service, or a
// key several people share. A label with no row is an active person. The
// name is the label on KeyLabel rows; ownership is an operator's
// statement, not proof of identity.
type AccessIdentity struct {
	ID        int64     `gorm:"primaryKey"`
	AccountID int64     `gorm:"not null;uniqueIndex:access_identities_name,priority:1"`
	Name      string    `gorm:"not null;size:80;uniqueIndex:access_identities_name,priority:2"`
	Kind      string    `gorm:"not null;default:'person';check:access_identities_kind_check,kind IN ('person','service','shared')"`
	Status    string    `gorm:"not null;default:'active';check:access_identities_status_check,status IN ('active','leaving','left')"`
	Note      string    `gorm:"not null;default:''"`
	UpdatedAt time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
}

// KeyBaselineVersion is one approval of a server's keys. KeyBaseline holds
// the current one; these rows keep every earlier approval and why.
type KeyBaselineVersion struct {
	ID         int64     `gorm:"primaryKey"`
	ServerID   int64     `gorm:"not null;uniqueIndex:key_baseline_versions_server,priority:1"`
	Version    int       `gorm:"not null;uniqueIndex:key_baseline_versions_server,priority:2"`
	AccountID  int64     `gorm:"not null;index"`
	Items      JSON      `gorm:"type:jsonb;not null"`
	Reason     string    `gorm:"not null;default:''"`
	ApprovedAt time.Time `gorm:"not null"`
	ApprovedBy int64     `gorm:"not null"`

	Server Server `gorm:"constraint:OnDelete:CASCADE"`
}

// KeyBaseline is the approved set of keys on one server. Later scans do not
// change it. Drift is reported, not applied.
type KeyBaseline struct {
	ServerID   int64     `gorm:"primaryKey"`
	AccountID  int64     `gorm:"not null;index"`
	Items      JSON      `gorm:"type:jsonb;not null"`
	ApprovedAt time.Time `gorm:"not null"`
	ApprovedBy int64     `gorm:"not null"`

	Server Server `gorm:"constraint:OnDelete:CASCADE"`
}

// MaintenanceWindow suppresses ordinary notifications. Collection still runs.
// host_key and new_key notices are not suppressed.
type MaintenanceWindow struct {
	ID        int64 `gorm:"primaryKey"`
	AccountID int64 `gorm:"not null;index"`
	GroupID   *int64
	ServerID  *int64
	Reason    string    `gorm:"not null"`
	StartsAt  time.Time `gorm:"not null"`
	EndsAt    time.Time `gorm:"not null"`
	CreatedBy int64     `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
}

// ServerCredential is a login kept for a server, only when the user chose
// "keep this login". Normal operation uses the account key.
type ServerCredential struct {
	ServerID  int64     `gorm:"primaryKey;autoIncrement:false"`
	Kind      string    `gorm:"not null;check:server_credentials_kind_check,kind IN ('password','private_key')"`
	Username  string    `gorm:"not null"`
	SecretEnc string    `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null"`

	Server Server `gorm:"constraint:OnDelete:CASCADE"`
}

// EnrollToken lets an enroll script install the helper for one server.
type EnrollToken struct {
	TokenHash []byte    `gorm:"primaryKey"`
	AccountID int64     `gorm:"not null"`
	ServerID  int64     `gorm:"not null;index"`
	CreatedBy int64     `gorm:"not null"`
	ExpiresAt time.Time `gorm:"not null"`
	UsedAt    *time.Time
	CreatedAt time.Time `gorm:"not null"`

	Server Server `gorm:"constraint:OnDelete:CASCADE"`
}

// ServerState is the latest snapshot of a server, one row per server.
type ServerState struct {
	ServerID int64 `gorm:"primaryKey;autoIncrement:false"`
	// Status is ok | warning | failing | unreachable | pending.
	Status        string `gorm:"not null;default:'pending'"`
	Reasons       JSON   `gorm:"type:jsonb;not null;default:'[]'"`
	Snapshot      JSON   `gorm:"type:jsonb"`
	Updates       JSON   `gorm:"type:jsonb"`
	UpdatesAt     *time.Time
	LastSeenAt    *time.Time
	LastAttemptAt *time.Time
	LastError     string `gorm:"not null;default:''"`
	MissedPolls   int    `gorm:"not null;default:0"`
	// HostKeyMismatch is set when the server presented a different key.
	// Every operation stops until a person accepts the new key.
	HostKeyMismatch  bool   `gorm:"not null;default:false"`
	PresentedHostKey string `gorm:"not null;default:''"`
	// Exposure is the board summary: public ports and a few flags.
	// It is not the user list. ExposureAt is when it was last attempted.
	Exposure   JSON `gorm:"type:jsonb"`
	ExposureAt *time.Time
	// SecuritySeen maps a pending security package to when it was first seen.
	SecuritySeen JSON `gorm:"type:jsonb"`
	UpdatedAt    time.Time

	Server Server `gorm:"constraint:OnDelete:CASCADE"`
}

// MetricSample is one poll's numbers, kept 48 hours.
type MetricSample struct {
	ServerID    int64     `gorm:"primaryKey;autoIncrement:false"`
	At          time.Time `gorm:"primaryKey"`
	CPUPercent  float64   `gorm:"not null"`
	MemPercent  float64   `gorm:"not null"`
	DiskPercent float64   `gorm:"not null"` // fullest real filesystem
	Load1       float64   `gorm:"not null"`
	RxBytes     int64     `gorm:"not null"`
	TxBytes     int64     `gorm:"not null"`

	Server Server `gorm:"constraint:OnDelete:CASCADE"`
}

// MetricHourly is the hourly average, kept 90 days.
type MetricHourly struct {
	ServerID    int64     `gorm:"primaryKey;autoIncrement:false"`
	Hour        time.Time `gorm:"primaryKey"`
	CPUPercent  float64   `gorm:"not null"`
	MemPercent  float64   `gorm:"not null"`
	DiskPercent float64   `gorm:"not null"`
	Load1       float64   `gorm:"not null"`
	Samples     int       `gorm:"not null"`

	Server Server `gorm:"constraint:OnDelete:CASCADE"`
}

// TableName keeps the plural readable.
func (MetricHourly) TableName() string { return "metric_hourly" }
