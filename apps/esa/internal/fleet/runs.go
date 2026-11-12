package fleet

import (
	"context"
	"time"

	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

// RunKind is the kind of maintenance a run does.
type RunKind string

const (
	RunMeasure RunKind = "measure"
	RunClean   RunKind = "clean"
	RunUpdate  RunKind = "update"
	RunReboot  RunKind = "reboot"
)

// Trigger records why a run started.
type Trigger string

const (
	TriggerSchedule Trigger = "schedule"
	TriggerManual   Trigger = "manual"
)

// RunOptions are the settings a run uses, copied from the group or the request.
type RunOptions struct {
	BuildCache       bool             `json:"buildCache,omitempty"`
	VSCode           bool             `json:"vscode,omitempty"`
	Journal          bool             `json:"journal,omitempty"`
	JournalSize      string           `json:"journalSize,omitempty"`
	UpdateMode       proto.UpdateMode `json:"updateMode,omitempty"`
	Hold             []string         `json:"hold,omitempty"`
	RebootIfRequired bool             `json:"rebootIfRequired,omitempty"`
	// BatchSize and MaxFailures shape update and reboot runs. 0 means 1.
	BatchSize   int `json:"batchSize,omitempty"`
	MaxFailures int `json:"maxFailures,omitempty"`
	// Checks are copied from the group when an update starts. Nil on a
	// request keeps the group's list.
	Checks []HealthCheck `json:"checks,omitempty"`
	// CanaryServerIDs run first. One failure there skips the other servers.
	CanaryServerIDs []int64 `json:"canaryServerIds,omitempty"`
	// ObserveSeconds is how long to repeat the checks on the canaries
	// before the other servers start. 0 runs the checks once.
	ObserveSeconds int `json:"observeSeconds,omitempty"`
}

// Limits on a group's rolling update settings.
const (
	maxBatchSize   = 50
	maxMaxFailures = 1000
)

// batching returns the batch size and failure limit, with 0 read as 1 so
// runs stored before these settings existed keep going one at a time.
func (o RunOptions) batching() (size, maxFailures int) {
	return max(o.BatchSize, 1), max(o.MaxFailures, 1)
}

// validBatching checks a batch size and failure limit, where 0 means 1.
func validBatching(size, maxFailures int) error {
	if size < 0 || size > maxBatchSize {
		return invalid("batchSize", "must be between 1 and %d", maxBatchSize)
	}
	if maxFailures < 0 || maxFailures > maxMaxFailures {
		return invalid("maxFailures", "must be between 1 and %d", maxMaxFailures)
	}
	return nil
}

// RunRequest starts a run on a group or on specific servers.
type RunRequest struct {
	Kind      RunKind
	GroupID   *int64
	ServerIDs []int64
	// Options overrides the group's settings. Nil uses the group's.
	Options *RunOptions
	// CanaryServerIDs run first on an update. One failure there skips the rest.
	CanaryServerIDs []int64
	// ObserveSeconds repeats the canary checks before the other servers start.
	ObserveSeconds int
}

// RunView is a run as the front doors show it.
type RunView struct {
	ID          int64
	Kind        RunKind
	Trigger     string
	Status      string
	GroupID     *int64
	GroupName   string
	Options     RunOptions
	StartedAt   time.Time
	FinishedAt  *time.Time
	Summary     string
	BytesFreed  int64
	RequestedBy *int64
	Servers     []RunServerView
}

// RunServerView is one server's part of a run.
type RunServerView struct {
	ServerID   int64
	ServerName string
	Status     string
	Error      string
	BytesFreed int64
	Detail     store.JSON
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// StartRun creates a run and executes it in the background.
func (s *Service) StartRun(ctx context.Context, a Actor, req RunRequest) (RunView, error) {
	scope := ScopeActions
	// The role is checked per group below: the effective role on the
	// target group, or on every server's group, must be admin.
	if !a.hasScope(scope) {
		return RunView{}, ErrForbidden
	}
	switch req.Kind {
	case RunMeasure, RunClean, RunUpdate, RunReboot:
	default:
		return RunView{}, invalid("kind", "must be measure, clean, update, or reboot")
	}

	// Servers may be the actor's own or shared with operate access, but a
	// run's servers always belong to one owning account, which owns the run.
	var group *store.Group
	var servers []store.Server
	var ownerID int64
	rc := s.roles(a)
	if req.GroupID != nil {
		g, acc, _, err := s.loadVisibleGroup(ctx, a, *req.GroupID)
		if err != nil {
			return RunView{}, err
		}
		if err := rc.canOperate(ctx, acc, &g.ID, RoleAdmin, scope); err != nil {
			return RunView{}, err
		}
		group, ownerID = &g, g.AccountID
		if err := s.db.WithContext(ctx).Where("account_id = ? AND group_id = ?", g.AccountID, g.ID).Order("name").Find(&servers).Error; err != nil {
			return RunView{}, err
		}
	} else {
		if len(req.ServerIDs) == 0 {
			return RunView{}, invalid("serverIds", "pick a group or at least one server")
		}
		if err := s.visibleServers(ctx, a).Where("servers.id IN ?", req.ServerIDs).Order("name").Find(&servers).Error; err != nil {
			return RunView{}, err
		}
		if len(servers) != len(uniqueIDs(req.ServerIDs)) {
			return RunView{}, ErrNotFound
		}
		ownerID = servers[0].AccountID
		for _, srv := range servers {
			if srv.AccountID != ownerID {
				return RunView{}, invalid("serverIds", "pick servers of one team; a run cannot mix your servers with shared ones or servers of different teams")
			}
		}
		for _, srv := range servers {
			acc, err := s.serverAccess(ctx, a, srv)
			if err != nil {
				return RunView{}, err
			}
			if err := rc.canOperate(ctx, acc, srv.GroupID, RoleAdmin, scope); err != nil {
				return RunView{}, err
			}
		}
	}
	if len(servers) == 0 {
		return RunView{}, invalid("groupId", "the group has no servers")
	}

	opts := RunOptions{BuildCache: true, VSCode: true, JournalSize: "200M", UpdateMode: proto.UpdateSecurity, BatchSize: 1, MaxFailures: 1}
	if group != nil {
		opts = RunOptions{
			BuildCache: group.CleanBuildCache, VSCode: group.CleanVSCode, Journal: group.CleanJournal, JournalSize: group.JournalSize,
			UpdateMode: proto.UpdateMode(group.UpdateMode), RebootIfRequired: group.RebootIfRequired,
			BatchSize: group.UpdateBatchSize, MaxFailures: group.UpdateMaxFailures,
		}
		_ = group.HoldPackages.Decode(&opts.Hold)
		_ = group.HealthChecks.Decode(&opts.Checks)
	}
	if req.Options != nil {
		// An override replaces the settings, except that a batch size,
		// failure limit, or check list left out keeps the group's.
		batch, maxFailures := opts.BatchSize, opts.MaxFailures
		checks := opts.Checks
		opts = *req.Options
		if opts.JournalSize == "" {
			opts.JournalSize = "200M"
		}
		if opts.UpdateMode == "" {
			opts.UpdateMode = proto.UpdateSecurity
		}
		if opts.BatchSize == 0 {
			opts.BatchSize = batch
		}
		if opts.MaxFailures == 0 {
			opts.MaxFailures = maxFailures
		}
		if opts.Checks == nil {
			opts.Checks = checks
		}
	}
	if err := validBatching(opts.BatchSize, opts.MaxFailures); err != nil {
		return RunView{}, err
	}
	opts.BatchSize, opts.MaxFailures = opts.batching()
	if req.Kind != RunUpdate && req.Kind != RunReboot {
		opts.BatchSize, opts.MaxFailures = 0, 0
	}
	if req.Kind == RunClean && !opts.BuildCache && !opts.VSCode && !opts.Journal {
		return RunView{}, invalid("options", "turn on at least one cleanup")
	}
	if !validJournalSize(opts.JournalSize) {
		return RunView{}, invalid("journalSize", "use a size like 200M or 1G")
	}
	if opts.UpdateMode != proto.UpdateSecurity && opts.UpdateMode != proto.UpdateAll {
		return RunView{}, invalid("updateMode", "must be security or all")
	}
	opts.Hold = cleanList(opts.Hold)
	for _, p := range opts.Hold {
		if !pkgNameRe.MatchString(p) {
			return RunView{}, invalid("hold", "%q is not a valid package name", p)
		}
	}
	if req.Kind == RunUpdate {
		checks, err := validChecks(opts.Checks)
		if err != nil {
			return RunView{}, err
		}
		opts.Checks = checks
		if len(req.CanaryServerIDs) > 0 {
			opts.CanaryServerIDs = req.CanaryServerIDs
		}
		if err := validCanaries(opts.CanaryServerIDs, servers); err != nil {
			return RunView{}, err
		}
		if req.ObserveSeconds > 0 {
			opts.ObserveSeconds = req.ObserveSeconds
		}
		if opts.ObserveSeconds < 0 || opts.ObserveSeconds > 600 {
			return RunView{}, invalid("observeSeconds", "must be from 0 to 600")
		}
		if opts.ObserveSeconds > 0 && len(opts.CanaryServerIDs) == 0 {
			return RunView{}, invalid("observeSeconds", "pick a canary to watch")
		}
		if opts.ObserveSeconds > 0 && len(opts.Checks) == 0 {
			return RunView{}, invalid("observeSeconds", "add a check to watch")
		}
	} else {
		opts.Checks, opts.CanaryServerIDs = nil, nil
		opts.ObserveSeconds = 0
	}

	trigger := TriggerManual
	if a.Source == SourceSystem {
		trigger = TriggerSchedule
	}
	run, err := s.createRun(ctx, a, ownerID, req.Kind, string(trigger), group, servers, opts, "running")
	if err != nil {
		return RunView{}, err
	}
	s.background(func(ctx context.Context) { s.execute(s.netCtx(ctx), run.ID) })
	return s.GetRun(ctx, a, run.ID)
}

// createRun records a run owned by ownerID, the account that owns servers.
// RequestedBy is the actor. When the actor works through a share, the audit
// entry goes to the owner with via_account_id and a copy stays with the actor.
func (s *Service) createRun(ctx context.Context, a Actor, ownerID int64, kind RunKind, trigger string, group *store.Group, servers []store.Server, opts RunOptions, status string) (store.Run, error) {
	run := store.Run{
		AccountID: ownerID, Kind: string(kind), Trigger: trigger, Status: status,
		Options: store.MustJSON(opts), RequestedBy: store.OptionalID(a.UserID), StartedAt: s.now(),
	}
	if group != nil {
		run.GroupID = &group.ID
	}
	if a.Source == SourceAPI || a.Source == SourceMCP {
		run.Trigger = string(a.Source)
	}
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		tx := q.Raw()
		if err := tx.Omit(clause.Associations).Create(&run).Error; err != nil {
			return err
		}
		for _, srv := range servers {
			rs := store.RunServer{RunID: run.ID, ServerID: srv.ID, Status: "queued", Detail: store.JSON("{}")}
			if status != "running" {
				rs.Status = "skipped"
			}
			if err := tx.Omit(clause.Associations).Create(&rs).Error; err != nil {
				return err
			}
		}
		target := "servers"
		if group != nil {
			target = group.Name
		}
		s.auditOn(ctx, q, a, ownerID, "run.start", target, map[string]any{"run_id": run.ID, "kind": kind, "servers": len(servers)})
		return nil
	})
	return run, err
}

// RunFilter narrows ListRuns.
type RunFilter struct {
	GroupID  *int64
	ServerID *int64
	Kind     string
	Status   string
	Limit    int
}

// ListRuns returns the newest runs first: the account's own runs and runs
// that touch servers shared with it.
func (s *Service) ListRuns(ctx context.Context, a Actor, f RunFilter) ([]RunView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return nil, err
	}
	q := s.visibleRuns(ctx, a).Preload("Group").Preload("Servers").Preload("Servers.Server").Order("started_at DESC, id DESC")
	if f.GroupID != nil {
		// Another account's group counts only when it is shared with the
		// actor, so the filter cannot confirm private groups.
		q = q.Where("runs.group_id = ? AND (runs.account_id = ? OR runs.group_id IN ("+sharedGroupIDsSQL+"))", *f.GroupID, a.AccountID, a.AccountID)
	}
	if f.ServerID != nil {
		// Only a server the actor can see matches, so the filter cannot
		// confirm private servers of the owner.
		q = q.Where("runs.id IN (?)", s.db.Model(&store.RunServer{}).Select("run_id").
			Where("server_id = ? AND (server_id IN (SELECT id FROM servers WHERE account_id = ?) OR server_id IN ("+sharedServerIDsSQL+"))",
				*f.ServerID, a.AccountID, a.AccountID))
	}
	if f.Kind != "" {
		q = q.Where("kind = ?", f.Kind)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	var runs []store.Run
	if err := q.Limit(f.Limit).Find(&runs).Error; err != nil {
		return nil, err
	}
	if err := s.hideUnsharedRunServers(ctx, a, runs); err != nil {
		return nil, err
	}
	out := make([]RunView, 0, len(runs))
	for _, r := range runs {
		out = append(out, toRunView(r))
	}
	return out, nil
}

// GetRun returns one run with every server's result.
func (s *Service) GetRun(ctx context.Context, a Actor, id int64) (RunView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil {
		return RunView{}, err
	}
	var r store.Run
	err := s.visibleRuns(ctx, a).Preload("Group").Preload("Servers", func(db *gormDB) *gormDB { return db.Order("id") }).
		Preload("Servers.Server").Where("runs.id = ?", id).Take(&r).Error
	if err != nil {
		return RunView{}, mapStore(err)
	}
	runs := []store.Run{r}
	if err := s.hideUnsharedRunServers(ctx, a, runs); err != nil {
		return RunView{}, err
	}
	return toRunView(runs[0]), nil
}

// hideUnsharedRunServers drops, from runs of other accounts, the servers
// that are not shared with the actor, so a receiving team never sees the
// owner's other servers. The group is hidden unless it is shared with the
// actor, and a finished run's summary counts only the servers kept.
func (s *Service) hideUnsharedRunServers(ctx context.Context, a Actor, runs []store.Run) error {
	var shared map[int64]bool
	var groups map[int64]store.GroupShare
	for i := range runs {
		if runs[i].AccountID == a.AccountID {
			continue
		}
		if shared == nil {
			var err error
			if shared, err = s.sharedServerIDs(ctx, a.AccountID); err != nil {
				return err
			}
			if groups, err = s.receivedShares(ctx, a.AccountID); err != nil {
				return err
			}
		}
		// Rebuild the summary only when the runner wrote it (other summaries,
		// like a missed schedule, name no servers).
		_, _, _, fullSummary := runTally(RunKind(runs[i].Kind), runs[i].Servers)
		kept := runs[i].Servers[:0]
		for _, rs := range runs[i].Servers {
			if shared[rs.ServerID] {
				kept = append(kept, rs)
			}
		}
		runs[i].Servers = kept
		if g := runs[i].GroupID; g != nil {
			if _, ok := groups[*g]; !ok {
				runs[i].GroupID, runs[i].Group = nil, nil
			}
		}
		if runs[i].Summary == fullSummary {
			_, _, _, runs[i].Summary = runTally(RunKind(runs[i].Kind), kept)
		}
	}
	return nil
}

func toRunView(r store.Run) RunView {
	v := RunView{
		ID: r.ID, Kind: RunKind(r.Kind), Trigger: r.Trigger, Status: r.Status, GroupID: r.GroupID,
		StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, Summary: r.Summary, RequestedBy: r.RequestedBy,
		Servers: []RunServerView{},
	}
	_ = r.Options.Decode(&v.Options)
	if r.Group != nil {
		v.GroupName = r.Group.Name
	}
	for _, rs := range r.Servers {
		v.BytesFreed += rs.BytesFreed
		v.Servers = append(v.Servers, RunServerView{
			ServerID: rs.ServerID, ServerName: rs.Server.Name, Status: rs.Status, Error: rs.Error,
			BytesFreed: rs.BytesFreed, Detail: rs.Detail, StartedAt: rs.StartedAt, FinishedAt: rs.FinishedAt,
		})
	}
	return v
}

func uniqueIDs(ids []int64) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
