package fleet

import (
	"context"
	"time"
)

// ServerStatus is the health of one server as shown on the overview.
type ServerStatus string

const (
	StatusOK          ServerStatus = "ok"
	StatusWarning     ServerStatus = "warning"
	StatusFailing     ServerStatus = "failing"
	StatusUnreachable ServerStatus = "unreachable"
	StatusPending     ServerStatus = "pending"
)

// ServerSummary is one row of the fleet overview.
type ServerSummary struct {
	ID         int64
	Name       string
	Host       string
	GroupName  string // empty when the server is in no group
	Status     ServerStatus
	LastSeenAt time.Time // zero when never seen
}

// Totals are the counts shown above the overview table.
type Totals struct {
	Servers, Groups, OK, Warning, Failing, Unreachable int
}

// Overview is the fleet page for one account.
type Overview struct {
	Totals  Totals
	Servers []ServerSummary
}

// Overview returns every server in the actor's account with its latest status.
//
// Servers and groups arrive with the add-server phase (migration 0002). Until
// then every account has an empty fleet; the shape of the response is final.
func (s *Service) Overview(ctx context.Context, a Actor) (Overview, error) {
	if _, err := s.Membership(ctx, a.UserID, a.AccountID); err != nil {
		return Overview{}, err
	}
	return Overview{Servers: []ServerSummary{}}, nil
}
