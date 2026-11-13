package fleet

import (
	"context"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// ServerView is a server with its latest state, as the front doors show it.
// It never contains credentials.
type ServerView struct {
	ID                 int64
	Name               string
	Host               string
	Port               int
	Username           string
	GroupID            *int64
	GroupName          string
	Enrollment         string
	HostKeyFingerprint string
	HelperVersion      string
	// HelperMissing are the actions a reinstall would add.
	HelperMissing       []string
	HelperCheckedAt     *time.Time
	Arch                string
	HostsControlPlane   bool
	ProtectedContainers []string
	HasStoredLogin      bool
	Note                string
	CreatedAt           time.Time
	// Access is own for the actor's servers, or the share's access.
	Access Access
	// OwnerTeam names the owning account of a shared server; empty for own.
	OwnerTeam string
	// YourRole is the actor's effective role on the server's group (see
	// roleOn), limited by the share when another account owns it.
	YourRole Role

	Status               ServerStatus
	Reasons              []string
	LastSeenAt           *time.Time
	LastError            string
	HostKeyMismatch      bool
	PresentedFingerprint string
	Snapshot             *proto.Status
	Updates              *proto.PendingUpdates
	UpdatesAt            *time.Time
	Exposure             *Exposure
}

// Totals are the counts shown above the overview table.
type Totals struct {
	Servers, Groups, OK, Warning, Failing, Unreachable, Pending int
}

// Overview is the fleet page for one account.
type Overview struct {
	Totals  Totals
	Servers []ServerView
}

// Overview returns every server the actor can see (own and shared) with its
// latest status, worst first.
func (s *Service) Overview(ctx context.Context, a Actor) (Overview, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return Overview{}, err
	}
	if err := s.member(ctx, a); err != nil {
		return Overview{}, err
	}
	views, err := s.serverViews(ctx, a, nil)
	if err != nil {
		return Overview{}, err
	}
	var groups int64
	if err := s.visibleGroups(ctx, a).Model(&store.Group{}).Count(&groups).Error; err != nil {
		return Overview{}, err
	}
	ov := Overview{Servers: views, Totals: Totals{Servers: len(views), Groups: int(groups)}}
	for _, v := range views {
		switch v.Status {
		case StatusOK:
			ov.Totals.OK++
		case StatusWarning:
			ov.Totals.Warning++
		case StatusFailing:
			ov.Totals.Failing++
		case StatusUnreachable:
			ov.Totals.Unreachable++
		default:
			ov.Totals.Pending++
		}
	}
	rank := map[ServerStatus]int{StatusFailing: 0, StatusUnreachable: 1, StatusWarning: 2, StatusPending: 3, StatusOK: 4}
	sortStable(ov.Servers, func(x, y ServerView) bool {
		if rank[x.Status] != rank[y.Status] {
			return rank[x.Status] < rank[y.Status]
		}
		return x.Name < y.Name
	})
	return ov, nil
}

// GetServer returns one server the actor can see.
func (s *Service) GetServer(ctx context.Context, a Actor, id int64) (ServerView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return ServerView{}, err
	}
	views, err := s.serverViews(ctx, a, []int64{id})
	if err != nil {
		return ServerView{}, err
	}
	if len(views) == 0 {
		return ServerView{}, ErrNotFound
	}
	return views[0], nil
}

// serverViews loads servers the actor can see, optionally limited to ids.
func (s *Service) serverViews(ctx context.Context, a Actor, ids []int64) ([]ServerView, error) {
	q := s.visibleServers(ctx, a).Preload("Group").Order("name, id")
	if ids != nil {
		q = q.Where("servers.id IN ?", ids)
	}
	var servers []store.Server
	if err := q.Find(&servers).Error; err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return []ServerView{}, nil
	}
	serverIDs := make([]int64, len(servers))
	for i, srv := range servers {
		serverIDs[i] = srv.ID
	}
	var states []store.ServerState
	if err := s.db.WithContext(ctx).Where("server_id IN ?", serverIDs).Find(&states).Error; err != nil {
		return nil, err
	}
	byID := make(map[int64]store.ServerState, len(states))
	for _, st := range states {
		byID[st.ServerID] = st
	}
	var stored []int64
	if err := s.db.WithContext(ctx).Model(&store.ServerCredential{}).Where("server_id IN ?", serverIDs).Pluck("server_id", &stored).Error; err != nil {
		return nil, err
	}
	hasLogin := map[int64]bool{}
	for _, id := range stored {
		hasLogin[id] = true
	}

	var owners []int64
	for _, srv := range servers {
		if srv.AccountID != a.AccountID {
			owners = append(owners, srv.AccountID)
		}
	}
	var received map[int64]store.GroupShare
	var ownerNames map[int64]string
	if len(owners) > 0 {
		var err error
		if received, err = s.receivedShares(ctx, a.AccountID); err != nil {
			return nil, err
		}
		if ownerNames, err = s.accountNames(ctx, owners); err != nil {
			return nil, err
		}
	}

	views := make([]ServerView, 0, len(servers))
	rc := s.roles(a)
	for _, srv := range servers {
		v := toView(srv, byID[srv.ID], hasLogin[srv.ID])
		v.Access = AccessOwn
		if srv.AccountID != a.AccountID {
			// visibleServers only returns these through a share of their group.
			sh, ok := received[deref(srv.GroupID)]
			if !ok || srv.Group == nil || srv.Group.AccountID != srv.AccountID {
				continue
			}
			v.Access, v.OwnerTeam = Access(sh.Access), ownerNames[srv.AccountID]
		}
		role, err := rc.forAccess(ctx, v.Access, srv.GroupID)
		if err != nil {
			return nil, err
		}
		v.YourRole = role
		views = append(views, v)
	}
	return views, nil
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func toView(srv store.Server, st store.ServerState, hasLogin bool) ServerView {
	v := ServerView{
		ID: srv.ID, Name: srv.Name, Host: srv.Host, Port: srv.Port, Username: srv.Username,
		GroupID: srv.GroupID, Enrollment: srv.Enrollment, HostKeyFingerprint: srv.HostKeyFingerprint,
		HelperVersion: srv.HelperVersion, Arch: srv.Arch, HostsControlPlane: srv.HostsControlPlane,
		HasStoredLogin: hasLogin, Note: srv.Note, CreatedAt: srv.CreatedAt, ProtectedContainers: []string{},
		Status: StatusPending, Reasons: []string{},
		HelperMissing: proto.Missing(storedCapabilities(srv)), HelperCheckedAt: srv.HelperCheckedAt,
	}
	_ = srv.ProtectedContainers.Decode(&v.ProtectedContainers)
	if srv.Group != nil {
		v.GroupName = srv.Group.Name
	}
	if st.ServerID == 0 {
		return v
	}
	v.Status = ServerStatus(st.Status)
	_ = st.Reasons.Decode(&v.Reasons)
	v.LastSeenAt, v.LastError, v.UpdatesAt = st.LastSeenAt, st.LastError, st.UpdatesAt
	v.HostKeyMismatch = st.HostKeyMismatch
	if st.HostKeyMismatch && st.PresentedHostKey != "" {
		if k, err := parseKey(st.PresentedHostKey); err == nil {
			v.PresentedFingerprint = fingerprint(k)
		}
	}
	if len(st.Snapshot) > 0 {
		var snap proto.Status
		if st.Snapshot.Decode(&snap) == nil {
			v.Snapshot = &snap
		}
	}
	if len(st.Exposure) > 0 {
		var ex Exposure
		if st.Exposure.Decode(&ex) == nil && !ex.CheckedAt.IsZero() {
			if len(ex.ListeningOnAllInterfaces) == 0 {
				ex.ListeningOnAllInterfaces = ex.PublicPorts
			}
			if st.ExposureAt != nil {
				ex.AttemptedAt = *st.ExposureAt
			} else {
				ex.AttemptedAt = ex.CheckedAt
			}
			v.Exposure = &ex
		}
	}
	if len(st.Updates) > 0 {
		var upd proto.PendingUpdates
		if st.Updates.Decode(&upd) == nil {
			v.Updates = &upd
		}
	}
	return v
}

// loadServer returns a server owned by the actor's account. Writes that
// change ownership-level data use it; shared servers are not found.
func (s *Service) loadServer(ctx context.Context, a Actor, id int64) (store.Server, error) {
	var srv store.Server
	err := s.scoped(ctx, a).Where("id = ?", id).Take(&srv).Error
	return srv, mapStore(err)
}

// loadVisibleServer returns a server the actor can see, own or shared, and
// the actor's access to it. Reads and operations use it; operations then
// check s.canOperateServer.
func (s *Service) loadVisibleServer(ctx context.Context, a Actor, id int64) (store.Server, Access, error) {
	var srv store.Server
	if err := s.visibleServers(ctx, a).Where("servers.id = ?", id).Take(&srv).Error; err != nil {
		return srv, AccessNone, mapStore(err)
	}
	acc, err := s.serverAccess(ctx, a, srv)
	if err != nil {
		return srv, AccessNone, err
	}
	return srv, acc, canReadServer(acc)
}

// checkGroup checks a group id belongs to the actor's own account. Shared
// groups are refused: a server only joins groups of its own account.
func (s *Service) checkGroup(ctx context.Context, a Actor, groupID *int64) error {
	if groupID == nil {
		return nil
	}
	var n int64
	if err := s.scoped(ctx, a).Model(&store.Group{}).Where("id = ?", *groupID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return invalid("groupId", "no such group")
	}
	return nil
}
