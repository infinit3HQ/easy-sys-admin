package fleet

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// ProbeResult is the host key a server presented, for the person adding it
// to compare with `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`.
type ProbeResult struct {
	HostKey     string
	Fingerprint string
	KeyType     string
}

// ProbeServer reads a server's host key without logging in.
func (s *Service) ProbeServer(ctx context.Context, a Actor, host string, port int) (ProbeResult, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return ProbeResult{}, err
	}
	host, err := validHost(host)
	if err != nil {
		return ProbeResult{}, err
	}
	if port, err = validPort(port); err != nil {
		return ProbeResult{}, err
	}
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	k, err := remote.ProbeHostKey(pctx, host, port)
	if err != nil {
		return ProbeResult{}, &UnavailableError{Message: err.Error()}
	}
	return ProbeResult{HostKey: remote.MarshalKey(k), Fingerprint: remote.Fingerprint(k), KeyType: k.Type()}, nil
}

// AddServerInput is the add-server form, path B: a one-time sudo login.
type AddServerInput struct {
	Name    string
	Host    string
	Port    int
	GroupID *int64
	// HostKey is the key from ProbeServer that the person accepted.
	HostKey string
	Login   remote.LoginAuth
	// KeepLogin stores the login (encrypted) for helper reinstalls. Off by default.
	KeepLogin         bool
	HostsControlPlane bool
}

// AddServer installs the helper with the given login and registers the
// server. The login is used once and discarded unless KeepLogin is set.
func (s *Service) AddServer(ctx context.Context, a Actor, in AddServerInput) (ServerView, error) {
	if err := a.can(RoleAdmin, ScopeAdmin); err != nil {
		return ServerView{}, err
	}
	srv, hostKey, err := s.validateNewServer(ctx, a, in.Name, in.Host, in.Port, in.GroupID, in.HostKey)
	if err != nil {
		return ServerView{}, err
	}
	if in.Login.User == "" {
		return ServerView{}, invalid("username", "enter the user to log in with")
	}
	if in.KeepLogin && !a.Role.AtLeast(RoleOwner) {
		return ServerView{}, invalid("keepLogin", "only account owners can store a login")
	}
	k, err := s.accountKey(ctx, a.AccountID)
	if err != nil {
		return ServerView{}, err
	}

	ictx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	res, err := remote.Install(ictx, remote.InstallRequest{
		Host: srv.Host, Port: srv.Port, HostKey: hostKey, Login: in.Login,
		AccountPublicKey: k.publicKey, Helper: s.helpers,
	})
	if err != nil {
		var mismatch *remote.HostKeyMismatchError
		if errors.As(err, &mismatch) {
			return ServerView{}, &ConflictError{Message: "the server's host key changed since you checked it (" + remote.Fingerprint(mismatch.Presented) + "); check the key again"}
		}
		return ServerView{}, &UnavailableError{Message: "install failed: " + err.Error()}
	}

	now := s.now()
	srv.Enrollment, srv.Arch, srv.HostsControlPlane = "ready", res.Arch, in.HostsControlPlane
	srv.HostKeyAcceptedBy, srv.HostKeyAcceptedAt = store.OptionalID(a.UserID), &now
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		tx := q.Raw()
		if err := tx.Omit(clause.Associations).Create(&srv).Error; err != nil {
			return mapStore(err)
		}
		if err := tx.Create(&store.ServerState{ServerID: srv.ID, Status: string(StatusPending), UpdatedAt: now}).Error; err != nil {
			return err
		}
		if in.KeepLogin {
			if err := s.storeLogin(ctx, q, srv.ID, in.Login); err != nil {
				return err
			}
		}
		s.audit(ctx, q, a, "server.add", srv.Name, map[string]any{
			"host": srv.Host, "port": srv.Port, "host_key": srv.HostKeyFingerprint, "method": "login", "kept_login": in.KeepLogin,
		})
		return nil
	})
	if err != nil {
		return ServerView{}, err
	}
	s.afterEnroll(srv.ID)
	return s.GetServer(ctx, a, srv.ID)
}

// validateNewServer checks the add-server fields and returns a server row
// ready to insert.
func (s *Service) validateNewServer(ctx context.Context, a Actor, name, host string, port int, groupID *int64, acceptedKey string) (store.Server, ssh.PublicKey, error) {
	var srv store.Server
	var err error
	if srv.Name, err = validName("name", name); err != nil {
		return srv, nil, err
	}
	if srv.Host, err = validHost(host); err != nil {
		return srv, nil, err
	}
	if srv.Port, err = validPort(port); err != nil {
		return srv, nil, err
	}
	if err := s.checkGroup(ctx, a, groupID); err != nil {
		return srv, nil, err
	}
	var dup int64
	if err := s.scoped(ctx, a).Model(&store.Server{}).Where("name = ?", srv.Name).Count(&dup).Error; err != nil {
		return srv, nil, err
	}
	if dup > 0 {
		return srv, nil, invalid("name", "a server named %q already exists", srv.Name)
	}
	srv.AccountID, srv.GroupID, srv.Username = a.AccountID, groupID, "esa"
	srv.ProtectedContainers = store.JSON("[]")
	srv.CreatedAt, srv.UpdatedAt = s.now(), s.now()
	if acceptedKey == "" {
		return srv, nil, nil
	}
	k, err := remote.ParseKey(acceptedKey)
	if err != nil {
		return srv, nil, invalid("hostKey", "not a valid SSH public key")
	}
	srv.HostKey, srv.HostKeyFingerprint = remote.MarshalKey(k), remote.Fingerprint(k)
	return srv, k, nil
}

func loginContext(serverID int64) string { return "server_login:" + strconv.FormatInt(serverID, 10) }

func (s *Service) storeLogin(ctx context.Context, q *store.Queries, serverID int64, l remote.LoginAuth) error {
	kind, secret := "password", []byte(l.Password)
	if len(l.PrivateKey) > 0 {
		kind, secret = "private_key", l.PrivateKey
	}
	sealed, err := s.box.Seal(secret, loginContext(serverID))
	if err != nil {
		return err
	}
	cred := store.ServerCredential{ServerID: serverID, Kind: kind, Username: l.User, SecretEnc: sealed, CreatedAt: s.now()}
	return q.Raw().WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Omit(clause.Associations).Create(&cred).Error
}

// storedLogin decrypts a kept login, if there is one.
func (s *Service) storedLogin(ctx context.Context, serverID int64) (remote.LoginAuth, bool, error) {
	var cred store.ServerCredential
	err := s.db.WithContext(ctx).Where("server_id = ?", serverID).Take(&cred).Error
	if mapStore(err) == ErrNotFound {
		return remote.LoginAuth{}, false, nil
	}
	if err != nil {
		return remote.LoginAuth{}, false, err
	}
	secret, err := s.box.Open(cred.SecretEnc, loginContext(serverID))
	if err != nil {
		return remote.LoginAuth{}, false, err
	}
	l := remote.LoginAuth{User: cred.Username}
	if cred.Kind == "private_key" {
		l.PrivateKey = secret
	} else {
		l.Password = string(secret)
	}
	return l, true, nil
}

// afterEnroll checks the new server answers as the esa user, then polls it.
func (s *Service) afterEnroll(serverID int64) {
	s.background(func(ctx context.Context) {
		if err := s.verifyHelper(ctx, serverID); err != nil {
			s.log.Warn("verify helper after enroll", "server_id", serverID, "err", err)
		}
		s.PollServer(ctx, serverID)
		s.RefreshUpdates(ctx, serverID, false)
	})
}

// verifyHelper runs `esa-helper version` as esa and records the version.
func (s *Service) verifyHelper(ctx context.Context, serverID int64) error {
	var srv store.Server
	if err := s.db.WithContext(ctx).Where("id = ?", serverID).Take(&srv).Error; err != nil {
		return err
	}
	ep, err := s.endpoint(ctx, srv)
	if err != nil {
		return err
	}
	var v struct {
		Version  string `json:"version"`
		Protocol int    `json:"protocol"`
		Arch     string `json:"arch"`
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.pool.Call(cctx, ep, &v, "version"); err != nil {
		return fmt.Errorf("helper check as esa: %w", err)
	}
	return s.db.WithContext(ctx).Model(&store.Server{}).Where("id = ?", serverID).
		Updates(map[string]any{"helper_version": v.Version, "arch": v.Arch, "updated_at": s.now()}).Error
}
