package store

import "time"

// Invite is a one-time link that adds a user to an organization. Only the SHA-256 of
// the link's token is stored. Accepting the invite deletes it.
type Invite struct {
	ID        int64  `gorm:"primaryKey"`
	AccountID int64  `gorm:"not null;index"`
	TokenHash []byte `gorm:"not null;uniqueIndex"`
	Role      string `gorm:"not null;check:invites_role_check,role IN ('owner','admin','viewer')"`
	// Email, when set, is the only address that can accept (lowercase).
	Email     string `gorm:"not null;default:''"`
	CreatedBy *int64
	CreatedAt time.Time `gorm:"not null"`
	ExpiresAt time.Time `gorm:"not null;index"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
	Creator *User   `gorm:"foreignKey:CreatedBy;constraint:OnDelete:SET NULL"`
}

// ShareLink is a one-time link that shares a group with another organization. The
// receiving organization is chosen when an admin of it accepts. Only the SHA-256 of
// the token is stored; accepting deletes the link.
type ShareLink struct {
	ID        int64  `gorm:"primaryKey"`
	GroupID   int64  `gorm:"not null;index"`
	AccountID int64  `gorm:"not null;index"` // the owning account, same as the group's
	TokenHash []byte `gorm:"not null;uniqueIndex"`
	Access    string `gorm:"not null;check:share_links_access_check,access IN ('view','operate')"`
	CreatedBy *int64
	CreatedAt time.Time `gorm:"not null"`
	ExpiresAt time.Time `gorm:"not null;index"`

	Group   Group   `gorm:"constraint:OnDelete:CASCADE"`
	Account Account `gorm:"constraint:OnDelete:CASCADE"`
	Creator *User   `gorm:"foreignKey:CreatedBy;constraint:OnDelete:SET NULL"`
}

// GroupShare gives another account access to a group and the servers in it.
// AccountID is the receiving account; the group's own AccountID is the owner.
type GroupShare struct {
	ID        int64  `gorm:"primaryKey"`
	GroupID   int64  `gorm:"not null;uniqueIndex:group_shares_group_account_key,priority:1"`
	AccountID int64  `gorm:"not null;index;uniqueIndex:group_shares_group_account_key,priority:2"`
	Access    string `gorm:"not null;check:group_shares_access_check,access IN ('view','operate')"`
	CreatedBy *int64
	CreatedAt time.Time `gorm:"not null"`

	Group   Group   `gorm:"constraint:OnDelete:CASCADE"`
	Account Account `gorm:"constraint:OnDelete:CASCADE"`
	Creator *User   `gorm:"foreignKey:CreatedBy;constraint:OnDelete:SET NULL"`
}

// Team is a named set of an organization's members who are admins of the
// team's server groups, on top of their organization role. The two
// built-in teams (admins and viewers) are not rows here: they are the
// organization roles, with names on the account.
type Team struct {
	ID        int64     `gorm:"primaryKey"`
	AccountID int64     `gorm:"not null;uniqueIndex:teams_account_name_key,priority:1"`
	Name      string    `gorm:"not null;size:100;uniqueIndex:teams_account_name_key,priority:2"`
	CreatedAt time.Time `gorm:"not null"`

	Account Account `gorm:"constraint:OnDelete:CASCADE"`
}

// TeamMember puts a member of the team's organization in the team.
// Removing the user from the organization must remove these rows too.
type TeamMember struct {
	TeamID    int64     `gorm:"primaryKey;autoIncrement:false"`
	UserID    int64     `gorm:"primaryKey;autoIncrement:false;index"`
	CreatedAt time.Time `gorm:"not null"`

	Team Team `gorm:"constraint:OnDelete:CASCADE"`
	User User `gorm:"constraint:OnDelete:CASCADE"`
}

// TeamGroup gives a team admin rights on a server group: one of its
// organization's groups, or a group another organization shares with it.
type TeamGroup struct {
	TeamID  int64 `gorm:"primaryKey;autoIncrement:false"`
	GroupID int64 `gorm:"primaryKey;autoIncrement:false;index"`

	Team  Team  `gorm:"constraint:OnDelete:CASCADE"`
	Group Group `gorm:"constraint:OnDelete:CASCADE"`
}
