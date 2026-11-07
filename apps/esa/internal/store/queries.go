package store

import (
	"context"
	"time"

	"gorm.io/gorm/clause"
)

// Conditions use explicit column expressions ("user_id = ?") instead of GORM
// struct conditions, because struct conditions silently drop zero values and
// would turn "WHERE user_id = 0" into no filter at all.

// UpsertUser creates the user on first sign-in or refreshes email, name, and
// last login on later ones.
func (q *Queries) UpsertUser(ctx context.Context, issuer, subject, email, name string, now time.Time) (User, error) {
	// Provider sign-in (Google, OIDC, dev) has already checked the address,
	// so the row is confirmed. Password accounts do not use this path.
	u := User{Issuer: issuer, Subject: subject, Email: email, Name: name, EmailConfirmed: true, CreatedAt: now, LastLoginAt: now}
	err := q.with(ctx).
		Clauses(
			clause.OnConflict{
				Columns:   []clause.Column{{Name: "issuer"}, {Name: "subject"}},
				DoUpdates: clause.AssignmentColumns([]string{"email", "name", "last_login_at", "email_confirmed"}),
			},
			// Return the stored row so CreatedAt is the original sign-up time.
			clause.Returning{},
		).
		Create(&u).Error
	return u, err
}

// GetUser returns the user with the given id, or ErrNotFound.
func (q *Queries) GetUser(ctx context.Context, id int64) (User, error) {
	var u User
	err := q.with(ctx).Where("id = ?", id).Take(&u).Error
	return u, notFound(err)
}

// CreateAccount inserts a new organization.
func (q *Queries) CreateAccount(ctx context.Context, name string, now time.Time) (Account, error) {
	a := Account{Name: name, CreatedAt: now}
	err := q.with(ctx).Omit(clause.Associations).Create(&a).Error
	return a, err
}

// CreatePersonalAccount inserts a user's personal space.
func (q *Queries) CreatePersonalAccount(ctx context.Context, userID int64, name string, now time.Time) (Account, error) {
	a := Account{Name: name, PersonalOf: &userID, CreatedAt: now}
	err := q.with(ctx).Omit(clause.Associations).Create(&a).Error
	return a, err
}

// AddMembership gives a user a role in an account.
func (q *Queries) AddMembership(ctx context.Context, accountID, userID int64, role string, now time.Time) error {
	m := Membership{AccountID: accountID, UserID: userID, Role: role, CreatedAt: now}
	return q.with(ctx).Omit(clause.Associations).Create(&m).Error
}

// ListMemberships returns every account the user belongs to, oldest account first.
func (q *Queries) ListMemberships(ctx context.Context, userID int64) ([]Membership, error) {
	var ms []Membership
	err := q.with(ctx).
		Joins("Account").
		Where("memberships.user_id = ?", userID).
		Order(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: clause.Column{Table: "Account", Name: "created_at"}},
			{Column: clause.Column{Table: "Account", Name: "id"}},
		}}).
		Find(&ms).Error
	return ms, err
}

// GetMembership returns the user's role in one account, or ErrNotFound.
func (q *Queries) GetMembership(ctx context.Context, userID, accountID int64) (Membership, error) {
	var m Membership
	err := q.with(ctx).
		Joins("Account").
		Where("memberships.user_id = ? AND memberships.account_id = ?", userID, accountID).
		Take(&m).Error
	return m, notFound(err)
}

// CreateSession stores a new session.
func (q *Queries) CreateSession(ctx context.Context, s Session) error {
	return q.with(ctx).Omit(clause.Associations).Create(&s).Error
}

// GetSession returns the session with the given token hash, or ErrNotFound.
// Expired sessions are returned too; the caller decides what to do with them.
func (q *Queries) GetSession(ctx context.Context, tokenHash []byte) (Session, error) {
	var s Session
	err := q.with(ctx).Where("token_hash = ?", tokenHash).Take(&s).Error
	return s, notFound(err)
}

// TouchSession records activity on a session.
func (q *Queries) TouchSession(ctx context.Context, tokenHash []byte, at time.Time) error {
	return q.with(ctx).Model(&Session{}).Where("token_hash = ?", tokenHash).Update("last_seen_at", at).Error
}

// SetSessionAccount changes the account a session works in.
func (q *Queries) SetSessionAccount(ctx context.Context, tokenHash []byte, accountID int64) error {
	return q.with(ctx).Model(&Session{}).Where("token_hash = ?", tokenHash).Update("account_id", accountID).Error
}

// DeleteSession removes one session.
func (q *Queries) DeleteSession(ctx context.Context, tokenHash []byte) error {
	return q.with(ctx).Where("token_hash = ?", tokenHash).Delete(&Session{}).Error
}

// DeleteExpiredSessions removes sessions that expired before now and returns how many.
func (q *Queries) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res := q.with(ctx).Where("expires_at <= ?", now).Delete(&Session{})
	return res.RowsAffected, res.Error
}

// InsertAudit appends one entry to the audit log.
func (q *Queries) InsertAudit(ctx context.Context, e AuditEntry) error {
	if e.Detail == "" {
		e.Detail = "{}"
	}
	return q.with(ctx).Omit(clause.Associations).Create(&e).Error
}

// MoveSessionsOff moves sessions working in accountID to another account
// their user belongs to: the user's personal space first, then their oldest
// other membership. Sessions with nowhere to go are deleted. userID limits
// the move to one user's sessions; 0 moves every user's.
func (q *Queries) MoveSessionsOff(ctx context.Context, accountID, userID int64) error {
	err := q.with(ctx).Exec(`
		UPDATE sessions s SET account_id = fb.account_id
		FROM (
		  SELECT DISTINCT ON (m.user_id) m.user_id, m.account_id
		  FROM memberships m
		  JOIN accounts acc ON acc.id = m.account_id
		  WHERE m.account_id <> ?
		    AND m.user_id IN (SELECT user_id FROM sessions WHERE account_id = ?)
		  ORDER BY m.user_id, COALESCE(acc.personal_of = m.user_id, false) DESC, acc.created_at, acc.id
		) fb
		WHERE s.account_id = ? AND s.user_id = fb.user_id AND (?::bigint = 0 OR s.user_id = ?)`,
		accountID, accountID, accountID, userID, userID).Error
	if err != nil {
		return err
	}
	return q.with(ctx).
		Where("account_id = ? AND (?::bigint = 0 OR user_id = ?)", accountID, userID, userID).
		Delete(&Session{}).Error
}
