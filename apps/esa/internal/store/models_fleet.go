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
	Arch          string `gorm:"not null;default:''"`

	// HostsControlPlane marks the server esa itself runs on. It is never
	// rebooted by a schedule.
	HostsControlPlane bool `gorm:"not null;default:false"`
	// ProtectedContainers are name prefixes; empty means the defaults.
	ProtectedContainers JSON `gorm:"type:jsonb;not null;default:'[]'"`

	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
	Group   *Group  `gorm:"constraint:OnDelete:SET NULL"`
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
	UpdatedAt        time.Time

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
