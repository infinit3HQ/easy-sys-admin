package fleet

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
)

var errMaintenanceOwnershipLost = errors.New("maintenance stopped: database lock connection was lost")

type maintenanceOwnershipKey struct{}

// Each advisory lock belongs to its original SQL connection. A fresh pool
// connection cannot prove ownership after a disconnect, even if queries work.
type maintenanceOwnership struct {
	mu     sync.Mutex
	conns  map[*sql.Conn]bool
	cancel context.CancelCauseFunc
}

func maintenanceContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	ownership := &maintenanceOwnership{conns: map[*sql.Conn]bool{}, cancel: cancel}
	ctx = context.WithValue(ctx, maintenanceOwnershipKey{}, ownership)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ownership.check(ctx) != nil {
					return
				}
			}
		}
	}()
	return ctx, func() { cancel(nil) }
}

func ownershipFrom(ctx context.Context) *maintenanceOwnership {
	owner, _ := ctx.Value(maintenanceOwnershipKey{}).(*maintenanceOwnership)
	return owner
}

func (o *maintenanceOwnership) check(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for conn := range o.conns {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		// sql.Conn never reconnects behind our back. An error here means this
		// worker can no longer prove that it holds its session's advisory lock.
		var one int
		err := conn.QueryRowContext(checkCtx, "SELECT 1").Scan(&one)
		cancel()
		if err != nil {
			o.cancel(errMaintenanceOwnershipLost)
			return errMaintenanceOwnershipLost
		}
	}
	return nil
}

func (s *Service) maintenanceCall(ctx context.Context, ep remote.Endpoint, out any, action string, args ...string) error {
	if owner := ownershipFrom(ctx); owner != nil {
		if err := owner.check(ctx); err != nil {
			return err
		}
	}
	return s.pool.Call(ctx, ep, out, action, args...)
}
