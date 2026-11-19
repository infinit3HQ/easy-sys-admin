package store

import "time"

// AgentCredential authenticates one outbound server. Revocation never falls
// back to SSH. Tokens are hashed; commands and results are sealed separately.
type AgentCredential struct {
	ServerID   int64     `gorm:"primaryKey;autoIncrement:false"`
	AccountID  int64     `gorm:"not null;index"`
	TokenHash  []byte    `gorm:"not null;uniqueIndex"`
	Generation int64     `gorm:"not null"`
	Revoked    bool      `gorm:"not null;default:false"`
	ExpiresAt  time.Time `gorm:"not null"`
	LastSeenAt *time.Time
	CreatedAt  time.Time `gorm:"not null"`
	Server     Server    `gorm:"constraint:OnDelete:CASCADE"`
}

type AgentTask struct {
	ID         int64     `gorm:"primaryKey"`
	ServerID   int64     `gorm:"not null;index:agent_tasks_queue,priority:1"`
	Generation int64     `gorm:"not null"`
	State      string    `gorm:"not null;index:agent_tasks_queue,priority:2;check:agent_tasks_state_check,state IN ('queued','claimed','done','cancelled')"`
	CommandEnc string    `gorm:"not null;default:''"`
	ResultEnc  string    `gorm:"not null;default:''"`
	ExpiresAt  time.Time `gorm:"not null;index"`
	CreatedAt  time.Time `gorm:"not null"`
	Server     Server    `gorm:"constraint:OnDelete:CASCADE"`
}
