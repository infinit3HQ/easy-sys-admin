package store

import "time"

// Models lists every table, in dependency order. Migrate passes this list
// to GORM AutoMigrate, so these structs are the only place the schema is defined.
func Models() []any {
	return []any{
		&Account{},
		&User{},
		&UserIdentity{},
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
		&Invite{},
		&ShareLink{},
		&GroupShare{},
		&Team{},
		&TeamMember{},
		&TeamGroup{},
		&AuthEmailToken{},
	}
}

// Account is a space that owns a fleet: a user's personal space or an
// organization.
// Every server, group, and run belongs to one account.
type Account struct {
	ID   int64  `gorm:"primaryKey"`
	Name string `gorm:"not null;size:200"`
	// PersonalOf is the user whose private space this is. It is NULL for
	// an organization. Each user has exactly one personal space, and it
	// never has other members.
	PersonalOf *int64    `gorm:"uniqueIndex"`
	CreatedAt  time.Time `gorm:"not null"`
	// Display names of the two built-in teams of an organization: the
	// members with the admin role and those with the viewer role.
	AdminTeamName  string `gorm:"not null;size:100;default:'Admins'"`
	ViewerTeamName string `gorm:"not null;size:100;default:'Viewers'"`

	PersonalUser *User `gorm:"foreignKey:PersonalOf;constraint:OnDelete:CASCADE"`
}

// Personal reports whether the account is a user's private space.
func (a Account) Personal() bool { return a.PersonalOf != nil }

// User is a person who signed in through the identity provider.
type User struct {
	ID      int64  `gorm:"primaryKey"`
	Issuer  string `gorm:"not null;uniqueIndex:users_issuer_subject_key,priority:1"`
	Subject string `gorm:"not null;uniqueIndex:users_issuer_subject_key,priority:2"`
	Email   string `gorm:"not null;default:''"`
	Name    string `gorm:"not null;default:''"`
	// PasswordHash is set when the person can sign in with a password.
	// A Google user can gain one later; it stays on this same row.
	PasswordHash string `gorm:"not null;default:''" json:"-"`
	// EmailConfirmed is true once a password account has opened its
	// confirmation link, or a provider has verified the address.
	// Existing rows default to true.
	EmailConfirmed bool      `gorm:"not null;default:true"`
	CreatedAt      time.Time `gorm:"not null"`
	LastLoginAt    time.Time `gorm:"not null"`
}

// UserIdentity is one way a person signs in. The user row keeps the issuer
// and subject from the first sign-in. A later Google sign-in with the same
// verified email adds a row here and joins that user instead of creating
// another account.
type UserIdentity struct {
	ID        int64     `gorm:"primaryKey"`
	UserID    int64     `gorm:"not null;index"`
	Issuer    string    `gorm:"not null;uniqueIndex:user_identities_issuer_subject_key,priority:1"`
	Subject   string    `gorm:"not null;uniqueIndex:user_identities_issuer_subject_key,priority:2"`
	CreatedAt time.Time `gorm:"not null"`

	User User `gorm:"constraint:OnDelete:CASCADE"`
}

// AuthEmailToken is a single-use confirmation or password-reset link.
// Only the hash of the token is stored.
type AuthEmailToken struct {
	ID        int64  `gorm:"primaryKey"`
	TokenHash []byte `gorm:"not null;uniqueIndex"`
	UserID    int64  `gorm:"not null;index"`
	Purpose   string `gorm:"not null;size:16;check:auth_email_tokens_purpose_check,purpose IN ('confirm','reset')"`
	// PasswordHash is set when the link adds a password to an account that
	// already signed in with a provider. It is applied once, when the link
	// is opened, and is never returned.
	PasswordHash string    `gorm:"not null;default:''" json:"-"`
	ExpiresAt    time.Time `gorm:"not null;index"`
	CreatedAt    time.Time `gorm:"not null"`

	User User `gorm:"constraint:OnDelete:CASCADE"`
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
