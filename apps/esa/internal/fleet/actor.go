package fleet

import (
	"context"
	"slices"
)

// Role is a user's permission level inside one account.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"
)

func (r Role) rank() int {
	switch r {
	case RoleOwner:
		return 3
	case RoleAdmin:
		return 2
	case RoleViewer:
		return 1
	}
	return 0
}

// AtLeast reports whether r grants at least the permissions of min.
func (r Role) AtLeast(min Role) bool { return r.rank() >= min.rank() }

// Source says which front door a request came through. It is written to the audit log.
type Source string

const (
	SourceWeb    Source = "web"
	SourceAPI    Source = "api"
	SourceMCP    Source = "mcp"
	SourceSystem Source = "system"
)

// Scope limits what an API token can do, on top of the user's role.
type Scope string

const (
	ScopeRead    Scope = "read"      // everything a viewer sees, except logs
	ScopeLogs    Scope = "logs:read" // server and container logs
	ScopeActions Scope = "actions"   // runs, service and container actions, reboots
	ScopeAdmin   Scope = "admin"     // servers, groups, alert rules, channels, certificates
)

// AllScopes lists every scope.
var AllScopes = []Scope{ScopeRead, ScopeLogs, ScopeActions, ScopeAdmin}

// Actor is the authenticated caller of a fleet operation: who, in which
// account, with which role, through which front door.
type Actor struct {
	UserID    int64
	AccountID int64
	Role      Role
	Source    Source
	// Scopes is nil for browser sessions (no extra limit) and the token's
	// scopes for API tokens.
	Scopes []Scope
	// TokenID is set for API tokens, for the audit log.
	TokenID int64
}

// System is the actor for scheduled work.
func System(accountID int64) Actor {
	return Actor{AccountID: accountID, Role: RoleOwner, Source: SourceSystem}
}

func (a Actor) hasScope(s Scope) bool {
	return a.Scopes == nil || slices.Contains(a.Scopes, s)
}

// can checks a role and a scope together.
func (a Actor) can(min Role, s Scope) error {
	if !a.Role.AtLeast(min) || !a.hasScope(s) {
		return ErrForbidden
	}
	return nil
}

type actorKey struct{}

// WithActor returns a context that carries the actor.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the actor stored in ctx, if any.
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok
}
