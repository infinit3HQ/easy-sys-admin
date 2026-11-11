package fleet

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// UpdateServerInput changes server settings. Nil fields stay as they are.
type UpdateServerInput struct {
	Name                *string
	GroupID             *int64
	ClearGroup          bool
	HostsControlPlane   *bool
	ProtectedContainers *[]string
	Note                *string
}

// UpdateServer changes a server's name, group, or protection settings.
func (s *Service) UpdateServer(ctx context.Context, a Actor, id int64, in UpdateServerInput) (ServerView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return ServerView{}, err
	}
	srv, err := s.loadServer(ctx, a, id)
	if err != nil {
		return ServerView{}, err
	}
	changes := map[string]any{"updated_at": s.now()}
	if in.Name != nil {
		name, err := validName("name", *in.Name)
		if err != nil {
			return ServerView{}, err
		}
		changes["name"] = name
	}
	if in.ClearGroup {
		changes["group_id"] = nil
	} else if in.GroupID != nil {
		if err := s.checkGroup(ctx, a, in.GroupID); err != nil {
			return ServerView{}, err
		}
		changes["group_id"] = *in.GroupID
	}
	if in.HostsControlPlane != nil {
		changes["hosts_control_plane"] = *in.HostsControlPlane
	}
	if in.ProtectedContainers != nil {
		changes["protected_containers"] = store.MustJSON(cleanList(*in.ProtectedContainers))
	}
	if in.Note != nil {
		note, err := cleanNote(*in.Note)
		if err != nil {
			return ServerView{}, err
		}
		changes["note"] = note
	}
	if err := s.db.WithContext(ctx).Model(&store.Server{}).Where("id = ? AND account_id = ?", srv.ID, a.AccountID).Updates(changes).Error; err != nil {
		return ServerView{}, mapStore(err)
	}
	delete(changes, "updated_at")
	s.audit(ctx, nil, a, "server.update", srv.Name, changes)
	return s.GetServer(ctx, a, id)
}

// cleanNote keeps a single line of at most 200 characters.
func cleanNote(s string) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > 200 {
		return "", invalid("note", "must be at most 200 characters")
	}
	return s, nil
}

// AcceptHostKey accepts the new host key a server presented. fingerprint
// must match the presented key, so the person confirms what they saw.
func (s *Service) AcceptHostKey(ctx context.Context, a Actor, id int64, fp string) (ServerView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return ServerView{}, err
	}
	srv, err := s.loadServer(ctx, a, id)
	if err != nil {
		return ServerView{}, err
	}
	var st store.ServerState
	if err := s.db.WithContext(ctx).Where("server_id = ?", srv.ID).Take(&st).Error; err != nil || !st.HostKeyMismatch || st.PresentedHostKey == "" {
		return ServerView{}, &ConflictError{Message: "this server has no changed host key to accept"}
	}
	k, err := remote.ParseKey(st.PresentedHostKey)
	if err != nil {
		return ServerView{}, err
	}
	if remote.Fingerprint(k) != fp {
		return ServerView{}, invalid("fingerprint", "does not match the key the server presented")
	}
	now := s.now()
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		tx := q.Raw()
		if err := tx.Model(&store.Server{}).Where("id = ?", srv.ID).Updates(map[string]any{
			"host_key": remote.MarshalKey(k), "host_key_fingerprint": fp,
			"host_key_accepted_by": a.UserID, "host_key_accepted_at": now, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&store.ServerState{}).Where("server_id = ?", srv.ID).Updates(map[string]any{
			"host_key_mismatch": false, "presented_host_key": "", "last_error": "", "missed_polls": 0,
		}).Error; err != nil {
			return err
		}
		s.audit(ctx, q, a, "server.accept_host_key", srv.Name, map[string]any{"old": srv.HostKeyFingerprint, "new": fp})
		return nil
	})
	if err != nil {
		return ServerView{}, err
	}
	s.pool.Forget(srv.ID)
	s.clearBackoff(srv.ID)
	s.background(func(ctx context.Context) { s.PollServer(ctx, srv.ID) })
	return s.GetServer(ctx, a, id)
}

// ReinstallHelper reinstalls or updates the helper. It uses login when given,
// otherwise the stored login.
func (s *Service) ReinstallHelper(ctx context.Context, a Actor, id int64, login *remote.LoginAuth) (ServerView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return ServerView{}, err
	}
	srv, err := s.loadServer(ctx, a, id)
	if err != nil {
		return ServerView{}, err
	}
	if srv.HostKey == "" {
		return ServerView{}, &ConflictError{Message: srv.Name + " has no accepted host key yet"}
	}
	if err := s.limitRate(a, "server.reinstall"); err != nil {
		return ServerView{}, err
	}
	var l remote.LoginAuth
	if login != nil && login.User != "" {
		l = *login
	} else {
		stored, ok, err := s.storedLogin(ctx, srv.ID)
		if err != nil {
			return ServerView{}, err
		}
		if !ok {
			return ServerView{}, invalid("login", "enter a sudo login; none is stored for this server")
		}
		l = stored
	}
	hk, err := remote.ParseKey(srv.HostKey)
	if err != nil {
		return ServerView{}, err
	}
	k, err := s.accountKey(ctx, srv.AccountID)
	if err != nil {
		return ServerView{}, err
	}
	ictx, cancel := context.WithTimeout(s.netCtx(ctx), 5*time.Minute)
	defer cancel()
	res, err := remote.Install(ictx, remote.InstallRequest{Host: srv.Host, Port: srv.Port, HostKey: hk, Login: l, AccountPublicKey: k.publicKey, Helper: s.helpers})
	if err != nil {
		var mismatch *remote.HostKeyMismatchError
		if errors.As(err, &mismatch) {
			s.recordMismatch(ctx, srv.ID, mismatch)
		}
		return ServerView{}, &UnavailableError{Message: "install failed: " + err.Error()}
	}
	if err := s.db.WithContext(ctx).Model(&store.Server{}).Where("id = ?", srv.ID).
		Updates(map[string]any{"enrollment": "ready", "arch": res.Arch, "updated_at": s.now()}).Error; err != nil {
		return ServerView{}, err
	}
	s.audit(ctx, nil, a, "server.reinstall_helper", srv.Name, nil)
	s.pool.Forget(srv.ID)
	s.clearBackoff(srv.ID)
	if err := s.verifyHelper(ctx, srv.ID); err != nil {
		return ServerView{}, &UnavailableError{Message: err.Error()}
	}
	s.background(func(ctx context.Context) { s.PollServer(ctx, srv.ID) })
	return s.GetServer(ctx, a, id)
}

// DeleteServer removes a server from esa. With uninstall, it first asks the
// helper to remove the esa user, its sudo rule, and itself.
func (s *Service) DeleteServer(ctx context.Context, a Actor, id int64, uninstall bool) error {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return err
	}
	srv, err := s.loadServer(ctx, a, id)
	if err != nil {
		return err
	}
	uninstalled := false
	if uninstall && srv.Enrollment == "ready" {
		if ep, err := s.endpoint(ctx, srv); err == nil {
			cctx, cancel := context.WithTimeout(s.netCtx(ctx), 30*time.Second)
			err = s.pool.Call(cctx, ep, nil, "uninstall")
			cancel()
			if err != nil {
				return &UnavailableError{Message: "could not remove esa from the server: " + err.Error() + ". Delete without removing, or fix the connection first."}
			}
			uninstalled = true
		}
	}
	s.pool.Forget(srv.ID)
	s.clearBackoff(srv.ID)
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		if err := q.Raw().Where("id = ? AND account_id = ?", srv.ID, a.AccountID).Delete(&store.Server{}).Error; err != nil {
			return err
		}
		s.audit(ctx, q, a, "server.delete", srv.Name, map[string]any{"uninstalled": uninstalled})
		return nil
	})
	return err
}
