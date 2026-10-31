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
	ID                  int64
	Name                string
	Host                string
	Port                int
	Username            string
	GroupID             *int64
	GroupName           string
	Enrollment          string
	HostKeyFingerprint  string
	HelperVersion       string
	Arch                string
	HostsControlPlane   bool
	ProtectedContainers []string
	HasStoredLogin      bool
	CreatedAt           time.Time

	Status               ServerStatus
	Reasons              []string
	LastSeenAt           *time.Time
	LastError            string
	HostKeyMismatch      bool
	PresentedFingerprint string
	Snapshot             *proto.Status
	Updates              *proto.PendingUpdates
	UpdatesAt            *time.Time
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

// Overview returns every server in the actor's account with its latest
// status, worst first.
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
	if err := s.scoped(ctx, a).Model(&store.Group{}).Count(&groups).Error; err != nil {
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

// GetServer returns one server in the actor's account.
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

// serverViews loads servers of the actor's account, optionally limited to ids.
func (s *Service) serverViews(ctx context.Context, a Actor, ids []int64) ([]ServerView, error) {
	q := s.scoped(ctx, a).Preload("Group").Order("name")
	if ids != nil {
		q = q.Where("id IN ?", ids)
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

	views := make([]ServerView, 0, len(servers))
	for _, srv := range servers {
		views = append(views, toView(srv, byID[srv.ID], hasLogin[srv.ID]))
	}
	return views, nil
}

func toView(srv store.Server, st store.ServerState, hasLogin bool) ServerView {
	v := ServerView{
		ID: srv.ID, Name: srv.Name, Host: srv.Host, Port: srv.Port, Username: srv.Username,
		GroupID: srv.GroupID, Enrollment: srv.Enrollment, HostKeyFingerprint: srv.HostKeyFingerprint,
		HelperVersion: srv.HelperVersion, Arch: srv.Arch, HostsControlPlane: srv.HostsControlPlane,
		HasStoredLogin: hasLogin, CreatedAt: srv.CreatedAt, ProtectedContainers: []string{},
		Status: StatusPending, Reasons: []string{},
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
	if len(st.Updates) > 0 {
		var upd proto.PendingUpdates
		if st.Updates.Decode(&upd) == nil {
			v.Updates = &upd
		}
	}
	return v
}

// loadServer returns a server in the actor's account.
func (s *Service) loadServer(ctx context.Context, a Actor, id int64) (store.Server, error) {
	var srv store.Server
	err := s.scoped(ctx, a).Where("id = ?", id).Take(&srv).Error
	return srv, mapStore(err)
}

// loadGroupID checks a group id belongs to the actor's account.
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
