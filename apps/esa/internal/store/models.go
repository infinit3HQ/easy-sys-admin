package store

import "time"

// Models lists every table, in dependency order. Migrate passes this list
// to GORM AutoMigrate, so these structs are the only place the schema is defined.
func Models() []any {
	return []any{
		&Account{},
		&User{},
		&Membership{},
		&Session{},
		&AuditEntry{},
		&AccountKey{},
		&Group{},
		&Server{},
		&ServerCredential{},
		&EnrollToken{},
		&ServerState{},
		&MetricSample{},
		&MetricHourly{},
		&Run{},
		&RunServer{},
		&AlertRule{},
		&Alert{},
		&NotifyChannel{},
		&CertTarget{},
		&APIToken{},
	}
}

// Account owns a fleet. Every server, group, and run belongs to one account.
type Account struct {
	ID        int64     `gorm:"primaryKey"`
	Name      string    `gorm:"not null;size:200"`
	CreatedAt time.Time `gorm:"not null"`
}

// User is a person who signed in through the identity provider.
type User struct {
	ID          int64     `gorm:"primaryKey"`
	Issuer      string    `gorm:"not null;uniqueIndex:users_issuer_subject_key,priority:1"`
	Subject     string    `gorm:"not null;uniqueIndex:users_issuer_subject_key,priority:2"`
	Email       string    `gorm:"not null;default:''"`
	Name        string    `gorm:"not null;default:''"`
	CreatedAt   time.Time `gorm:"not null"`
	LastLoginAt time.Time `gorm:"not null"`
}

// Membership is a user's role in an account.
type Membership struct {
	AccountID int64     `gorm:"primaryKey;autoIncrement:false"`
	UserID    int64     `gorm:"primaryKey;autoIncrement:false;index"`
	Role      string    `gorm:"not null;check:memberships_role_check,role IN ('owner','admin','viewer')"`
	CreatedAt time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
	User    User    `gorm:"constraint:OnDelete:CASCADE"`
}

// Session is a signed-in browser. TokenHash is the SHA-256 of the cookie
// value; the token itself is never stored.
type Session struct {
	TokenHash  []byte    `gorm:"primaryKey"`
	UserID     int64     `gorm:"not null;index"`
	AccountID  int64     `gorm:"not null"`
	CreatedAt  time.Time `gorm:"not null"`
	LastSeenAt time.Time `gorm:"not null"`
	ExpiresAt  time.Time `gorm:"not null;index"`

	User    User    `gorm:"constraint:OnDelete:CASCADE"`
	Account Account `gorm:"constraint:OnDelete:CASCADE"`
}

// AuditEntry records who did what, from which front door.
type AuditEntry struct {
	ID        int64     `gorm:"primaryKey"`
	AccountID *int64    `gorm:"index:audit_log_account_time,priority:1"`
	UserID    *int64    `gorm:"index"`
	Action    string    `gorm:"not null"`
	Target    string    `gorm:"not null;default:''"`
	Detail    string    `gorm:"type:jsonb;not null;default:'{}'"`
	Source    string    `gorm:"not null;check:audit_log_source_check,source IN ('web','api','mcp','system')"`
	CreatedAt time.Time `gorm:"not null;index:audit_log_account_time,priority:2,sort:desc"`

	Account *Account `gorm:"constraint:OnDelete:CASCADE"`
	User    *User    `gorm:"constraint:OnDelete:SET NULL"`
}

// TableName keeps the table name singular, as "audit_log" reads better than "audit_entries".
func (AuditEntry) TableName() string { return "audit_log" }

// OptionalID turns 0 into NULL for optional foreign keys.
func OptionalID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}
