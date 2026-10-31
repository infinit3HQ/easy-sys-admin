package fleet

import "context"

// Role is a user's permission level inside one account.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"
)

// Source says which front door a request came through. It is written to the audit log.
type Source string

const (
	SourceWeb    Source = "web"
	SourceAPI    Source = "api"
	SourceMCP    Source = "mcp"
	SourceSystem Source = "system"
)

// Actor is the authenticated caller of a fleet operation: who, in which
// account, with which role, through which front door.
type Actor struct {
	UserID    int64
	AccountID int64
	Role      Role
	Source    Source
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
