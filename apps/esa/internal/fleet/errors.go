package fleet

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

var (
	// ErrForbidden means the actor may not do this.
	ErrForbidden = errors.New("fleet: forbidden")
	// ErrNotFound means the target does not exist in the actor's account.
	// Targets in other accounts also return ErrNotFound, so ids cannot be probed.
	ErrNotFound = errors.New("fleet: not found")
	// ErrSignInNotAllowed means the identity is not on the allowed list.
	ErrSignInNotAllowed = errors.New("fleet: sign-in not allowed for this user")
)

// ValidationError is a bad input field.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// ConflictError means the change clashes with existing state, for example a
// duplicate name or a run already in progress.
type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }

// UnavailableError means a server could not be reached or refused the
// operation. Message is safe to show to the user.
type UnavailableError struct{ Message string }

func (e *UnavailableError) Error() string { return e.Message }

// mapStore turns store errors into fleet errors.
func mapStore(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		return ErrNotFound
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return &ConflictError{Message: "an item with that name already exists"}
	}
	return err
}

// errPersonOnly refuses a change that weakens a safety rule unless a person
// makes it in the web app.
func errPersonOnly(what string) error {
	return invalid("planRequired", "only a person signed in to the web app can %s", what)
}
