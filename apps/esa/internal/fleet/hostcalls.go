package fleet

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// call runs a helper action on a server in the actor's account.
func (s *Service) call(ctx context.Context, a Actor, serverID int64, timeout time.Duration, out any, action string, args ...string) (store.Server, error) {
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return srv, err
	}
	if err := s.blockedByHostKey(ctx, srv); err != nil {
		return srv, err
	}
	ep, err := s.endpoint(ctx, srv)
	if err != nil {
		return srv, err
	}
	cctx, cancel := context.WithTimeout(s.netCtx(ctx), timeout)
	defer cancel()
	if err := s.pool.Call(cctx, ep, out, action, args...); err != nil {
		return srv, s.hostErr(ctx, srv, err)
	}
	return srv, nil
}

func (s *Service) blockedByHostKey(ctx context.Context, srv store.Server) error {
	var n int64
	s.db.WithContext(ctx).Model(&store.ServerState{}).Where("server_id = ? AND host_key_mismatch", srv.ID).Count(&n)
	if n > 0 {
		return &ConflictError{Message: srv.Name + "'s host key changed. Accept the new key on the server page before anything else runs."}
	}
	return nil
}

func protectArgs(srv store.Server) []string {
	var list []string
	_ = srv.ProtectedContainers.Decode(&list)
	if len(list) == 0 {
		return nil
	}
	return []string{"--protect", strings.Join(list, ",")}
}

// Services lists systemd services on a server.
func (s *Service) Services(ctx context.Context, a Actor, serverID int64) ([]proto.Service, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	var out []proto.Service
	_, err := s.call(ctx, a, serverID, time.Minute, &out, "services")
	return out, err
}

// Containers lists Docker containers on a server.
func (s *Service) Containers(ctx context.Context, a Actor, serverID int64) ([]proto.Container, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return nil, err
	}
	var out []proto.Container
	_, err = s.call(ctx, a, serverID, time.Minute, &out, "containers", protectArgs(srv)...)
	return out, err
}

// ImageCheck compares container images with their registries.
func (s *Service) ImageCheck(ctx context.Context, a Actor, serverID int64) ([]proto.ImageCheck, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	var out []proto.ImageCheck
	_, err := s.call(ctx, a, serverID, 5*time.Minute, &out, "image-check")
	return out, err
}

// LogQuery selects log lines.
type LogQuery struct {
	Unit, Container, Grep string
	Lines, Priority       int
}

// Logs returns journal or container log lines. Logs can contain secrets, so
// they need the admin role and the logs:read scope.
func (s *Service) Logs(ctx context.Context, a Actor, serverID int64, q LogQuery) ([]proto.LogLine, error) {
	if err := a.can(RoleAdmin, ScopeLogs); err != nil {
		return nil, err
	}
	args := []string{"--lines", strconv.Itoa(q.Lines), "--priority", strconv.Itoa(q.Priority)}
	switch {
	case q.Unit != "" && q.Container == "":
		args = append(args, "--unit", q.Unit)
	case q.Container != "" && q.Unit == "":
		args = append(args, "--container", q.Container)
	default:
		return nil, invalid("unit", "give a unit or a container")
	}
	if q.Grep != "" {
		args = append(args, "--grep", q.Grep)
	}
	var out []proto.LogLine
	srv, err := s.call(ctx, a, serverID, time.Minute, &out, "logs", args...)
	if err == nil {
		s.audit(ctx, nil, a, "server.read_logs", srv.Name, map[string]any{"unit": q.Unit, "container": q.Container})
	}
	return out, err
}

// Access reports users, keys, and logins on a server.
func (s *Service) Access(ctx context.Context, a Actor, serverID int64) (proto.AccessReport, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return proto.AccessReport{}, err
	}
	var out proto.AccessReport
	_, err := s.call(ctx, a, serverID, time.Minute, &out, "access")
	return out, err
}

// Security reports listening ports and protection services on a server.
func (s *Service) Security(ctx context.Context, a Actor, serverID int64) (proto.SecurityReport, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return proto.SecurityReport{}, err
	}
	var out proto.SecurityReport
	_, err := s.call(ctx, a, serverID, time.Minute, &out, "security")
	return out, err
}

// Jobs lists cron entries and timers on a server.
func (s *Service) Jobs(ctx context.Context, a Actor, serverID int64) (proto.JobsReport, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return proto.JobsReport{}, err
	}
	var out proto.JobsReport
	_, err := s.call(ctx, a, serverID, time.Minute, &out, "jobs")
	// Cron command lines often hold secrets (tokens, passwords in URLs), so
	// they need the same access as logs. Schedules stay visible.
	if a.can(RoleAdmin, ScopeLogs) != nil {
		for i := range out.Cron {
			out.Cron[i].Command = ""
		}
	}
	return out, err
}

// CheckUpdates refreshes the package lists on a server and returns the
// pending updates.
func (s *Service) CheckUpdates(ctx context.Context, a Actor, serverID int64) (ServerView, error) {
	if err := a.can(RoleAdmin, ScopeActions); err != nil {
		return ServerView{}, err
	}
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return ServerView{}, err
	}
	if err := s.blockedByHostKey(ctx, srv); err != nil {
		return ServerView{}, err
	}
	if err := s.RefreshUpdates(ctx, serverID, true); err != nil {
		return ServerView{}, err
	}
	s.PollServer(ctx, serverID)
	return s.GetServer(ctx, a, serverID)
}

// ServiceAction starts, stops, or restarts a systemd service.
func (s *Service) ServiceAction(ctx context.Context, a Actor, serverID int64, unit, action string) error {
	if err := a.can(RoleAdmin, ScopeActions); err != nil {
		return err
	}
	srv, err := s.call(ctx, a, serverID, 2*time.Minute, nil, "service", "--unit", unit, "--action", action)
	s.audit(ctx, nil, a, "service."+action, srv.Name+"/"+unit, map[string]any{"ok": err == nil})
	return err
}

// ContainerAction starts, stops, or restarts a container. confirm must be
// the container name for protected containers.
func (s *Service) ContainerAction(ctx context.Context, a Actor, serverID int64, name, action, confirm string) error {
	if err := a.can(RoleAdmin, ScopeActions); err != nil {
		return err
	}
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return err
	}
	args := append([]string{"--name", name, "--action", action, "--confirm", confirm}, protectArgs(srv)...)
	_, err = s.call(ctx, a, serverID, 2*time.Minute, nil, "container", args...)
	s.audit(ctx, nil, a, "container."+action, srv.Name+"/"+name, map[string]any{"ok": err == nil})
	return err
}

// ContainerUpdate pulls a compose service's image and recreates it.
func (s *Service) ContainerUpdate(ctx context.Context, a Actor, serverID int64, name, confirm string) (string, error) {
	if err := a.can(RoleAdmin, ScopeActions); err != nil {
		return "", err
	}
	srv, err := s.loadServer(ctx, a, serverID)
	if err != nil {
		return "", err
	}
	var out struct {
		Output string `json:"output"`
	}
	args := append([]string{"--name", name, "--confirm", confirm}, protectArgs(srv)...)
	_, err = s.call(ctx, a, serverID, 15*time.Minute, &out, "container-update", args...)
	s.audit(ctx, nil, a, "container.update", srv.Name+"/"+name, map[string]any{"ok": err == nil})
	return out.Output, err
}
