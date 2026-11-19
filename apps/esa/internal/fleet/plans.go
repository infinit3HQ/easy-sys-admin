package fleet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

const planApprovalTTL = 30 * time.Minute

// PlanBody is the only authority in a fix plan. Free text cannot add a step.
type PlanBody struct {
	Kind             RunKind  `json:"kind"`
	GroupID          *int64   `json:"groupId,omitempty"`
	ServerIDs        []int64  `json:"serverIds,omitempty"`
	UpdateMode       string   `json:"updateMode,omitempty"`
	Hold             []string `json:"hold,omitempty"`
	RebootIfRequired bool     `json:"rebootIfRequired,omitempty"`
	ObserveSeconds   int      `json:"observeSeconds,omitempty"`
	CanaryServerIDs  []int64  `json:"canaryServerIds,omitempty"`
	BuildCache       bool     `json:"buildCache,omitempty"`
	VSCode           bool     `json:"vscode,omitempty"`
	Journal          bool     `json:"journal,omitempty"`
	JournalSize      string   `json:"journalSize,omitempty"`
	// Packages are the packages the plan approves: a chosen subset of the
	// pending list, or all of it when none were chosen. The helper installs
	// nothing else.
	Packages []string `json:"packages,omitempty"`
	// Pending is every pending package when the plan was proposed. A
	// different list at execution invalidates the plan. Plans stored
	// before this field compare against Packages.
	Pending []string `json:"pending,omitempty"`
	// WaitAfterCanaries pauses the plan's run after its canaries until a
	// person continues it.
	WaitAfterCanaries bool `json:"waitAfterCanaries,omitempty"`
	// PinnedServerIDs are the servers the plan covered when it was
	// proposed. A server added to or removed from the group since
	// invalidates the plan, so an approval never reaches a new server.
	PinnedServerIDs []int64 `json:"pinnedServerIds,omitempty"`
}

// PlanView is a plan as the API shows it.
type PlanView struct {
	Approvals         int
	ApprovalsRequired int
	ID                int64
	Status            string
	Fingerprint       string
	Body              PlanBody
	ExpiresAt         *time.Time
	RunID             *int64
	Reason            string
	CreatedAt         time.Time
}

func planFingerprint(body PlanBody) (string, error) {
	slices.Sort(body.ServerIDs)
	slices.Sort(body.CanaryServerIDs)
	slices.Sort(body.Hold)
	slices.Sort(body.PinnedServerIDs)
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// canPropose matches who may start a run: an admin, and for a token the
// actions scope without logs:read, as docs/mcp.md describes.
func (s *Service) canPropose(a Actor) error {
	if err := a.can(RoleAdmin, ScopeActions); err != nil {
		return err
	}
	if a.Scopes != nil && a.hasScope(ScopeLogs) {
		return ErrForbidden
	}
	return nil
}

// ProposePlan stores a typed plan. It does not touch a server.
func (s *Service) ProposePlan(ctx context.Context, a Actor, body PlanBody) (PlanView, error) {
	if err := s.canPropose(a); err != nil {
		return PlanView{}, err
	}
	switch body.Kind {
	case RunMeasure, RunClean, RunUpdate:
	default:
		return PlanView{}, invalid("kind", "must be measure, clean, or update")
	}
	if body.GroupID == nil && len(body.ServerIDs) == 0 {
		return PlanView{}, invalid("serverIds", "pick a group or at least one server")
	}
	if body.GroupID != nil && len(body.ServerIDs) > 0 {
		return PlanView{}, invalid("serverIds", "pick a group or servers, not both")
	}
	if body.Kind == RunClean && !body.BuildCache && !body.VSCode && !body.Journal {
		return PlanView{}, invalid("kind", "turn on at least one cleanup")
	}
	if body.Kind != RunUpdate {
		body.UpdateMode, body.Hold, body.RebootIfRequired = "", nil, false
		body.ObserveSeconds, body.CanaryServerIDs, body.WaitAfterCanaries = 0, nil, false
		body.Packages = nil
	}
	if body.WaitAfterCanaries && len(body.CanaryServerIDs) == 0 {
		return PlanView{}, invalid("waitAfterCanaries", "pick a canary to wait after")
	}
	target, err := s.planTarget(ctx, a.AccountID, body)
	if err != nil {
		return PlanView{}, err
	}
	body.PinnedServerIDs = serverIDs(target)
	if body.Kind == RunUpdate {
		names, err := s.pendingNames(ctx, target, body.UpdateMode)
		if err != nil {
			return PlanView{}, err
		}
		if len(names) == 0 {
			return PlanView{}, invalid("packages", "no %s updates are pending on these servers, so there is nothing to approve", modeWord(body.UpdateMode))
		}
		chosen, err := choosePackages(body.Packages, names)
		if err != nil {
			return PlanView{}, err
		}
		body.Pending, body.Packages = names, chosen
	}
	fp, err := planFingerprint(body)
	if err != nil {
		return PlanView{}, err
	}
	need, err := s.planApprovalRequirement(ctx, s.db, a.AccountID, body)
	if err != nil {
		return PlanView{}, err
	}
	now := s.now()
	row := store.FixPlan{
		ApprovalsRequired: need,
		AccountID:         a.AccountID, Status: "submitted", Fingerprint: fp, Body: store.MustJSON(body),
		CreatedBy: a.UserID, ProposerTokenID: a.TokenID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Omit("Account").Create(&row).Error; err != nil {
		return PlanView{}, err
	}
	s.audit(ctx, nil, a, "plan.propose", fp, map[string]any{"id": row.ID, "kind": body.Kind})
	return toPlan(row, body), nil
}

// ApprovePlan records a human approval. A token cannot approve, including an admin token.
// A browser session older than 15 minutes must sign in again first.
func (s *Service) ApprovePlan(ctx context.Context, a Actor, id int64) (PlanView, error) {
	if !a.isPerson() || !a.Role.AtLeast(RoleAdmin) {
		return PlanView{}, ErrForbidden
	}
	if a.SignedInAt != nil && s.now().Sub(*a.SignedInAt) > 15*time.Minute {
		return PlanView{}, invalid("status", "sign in again, then approve. This session is more than 15 minutes old")
	}

	var row store.FixPlan
	var body PlanBody
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		tx := q.Raw().WithContext(ctx)
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND account_id = ?", id, a.AccountID).Take(&row).Error; err != nil {
			return mapStore(err)
		}
		if row.Status != "submitted" {
			return invalid("status", "only a submitted plan can be approved")
		}
		if err := row.Body.Decode(&body); err != nil {
			return err
		}
		fp, err := planFingerprint(body)
		if err != nil || fp != row.Fingerprint {
			return invalid("status", "the plan body does not match its fingerprint")
		}
		required, err := s.planApprovalRequirement(ctx, tx, a.AccountID, body)
		if err != nil {
			return err
		}
		row.ApprovalsRequired = max(required, row.ApprovalsRequired)
		active, err := memberHolds(ctx, q, a.UserID, a.AccountID, RoleAdmin)
		if err != nil {
			return err
		}
		if !active {
			return ErrForbidden
		}
		// PostgreSQL stores microseconds. Return exactly the timestamp that
		// will be read by the second approver, even on nanosecond clocks.
		now := s.now().UTC().Truncate(time.Microsecond)
		if row.ApprovedBy == nil {
			row.ApprovedBy, row.ApprovedAt = &a.UserID, &now
			expiry := now.Add(planApprovalTTL)
			row.ExpiresAt = &expiry
		} else {
			if row.ExpiresAt == nil || !row.ExpiresAt.After(now) {
				return invalid("status", "the first approval expired; propose a new plan")
			}
			active, err := memberHolds(ctx, q, *row.ApprovedBy, a.AccountID, RoleAdmin)
			if err != nil {
				return err
			}
			if !active {
				return invalid("status", "the first approver is no longer an admin; propose a new plan")
			}
			if *row.ApprovedBy == a.UserID {
				return nil
			}
			row.SecondApprovedBy = &a.UserID
		}
		if row.ApprovalsRequired == 1 || row.SecondApprovedBy != nil {
			row.Status = "approved"
		}
		row.UpdatedAt = now
		if err := tx.Omit("Account").Save(&row).Error; err != nil {
			return err
		}
		s.audit(ctx, q, a, "plan.approve", row.Fingerprint, map[string]any{"id": row.ID, "expires": row.ExpiresAt, "required": row.ApprovalsRequired})
		return nil
	})
	if err != nil {
		return PlanView{}, err
	}
	return toPlan(row, body), nil
}

// RejectPlan records a human rejection. A token cannot reject.
func (s *Service) RejectPlan(ctx context.Context, a Actor, id int64, reason string) (PlanView, error) {
	if !a.isPerson() || !a.Role.AtLeast(RoleAdmin) {
		return PlanView{}, ErrForbidden
	}
	row, body, err := s.loadPlan(ctx, a, id)
	if err != nil {
		return PlanView{}, err
	}
	if row.Status != "submitted" {
		return PlanView{}, invalid("status", "only a submitted plan can be rejected")
	}
	changed, err := s.rejectSubmitted(ctx, row.ID, reason)
	if err != nil {
		return PlanView{}, err
	}
	if !changed {
		return PlanView{}, invalid("status", "only a submitted plan can be rejected")
	}
	s.audit(ctx, nil, a, "plan.reject", row.Fingerprint, map[string]any{"id": row.ID})
	row.Status, row.Reason = "rejected", reason
	return toPlan(row, body), nil
}

// ExecutePlan starts the approved plan once. A later call returns the same run.
// A token that can read logs cannot execute.
//
// The plan and its run are committed together before the worker starts.
func (s *Service) ExecutePlan(ctx context.Context, a Actor, id int64) (RunView, error) {
	if err := a.can(RoleAdmin, ScopeActions); err != nil {
		return RunView{}, err
	}
	if a.hasScope(ScopeLogs) && a.Scopes != nil {
		return RunView{}, ErrForbidden
	}
	var claim planClaim
	var run store.Run
	err := s.store.InTx(ctx, func(q *store.Queries) error {
		var err error
		claim, err = s.claimPlan(ctx, q.Raw(), a, id)
		if err != nil || claim.runID != nil {
			return err
		}
		run, err = s.prepareRun(ctx, a, planRunRequest(claim.body), q)
		if err != nil {
			return err
		}
		if err := q.Raw().Model(&claim.row).Updates(map[string]any{
			"status": "executing", "run_id": run.ID, "updated_at": s.now(),
		}).Error; err != nil {
			return err
		}
		s.auditOn(ctx, q, a, a.AccountID, "plan.execute", claim.row.Fingerprint, map[string]any{"id": id, "run": run.ID})
		return nil
	})
	if claim.refuse != nil {
		s.setPlanStatus(ctx, a, claim.row, claim.refuse.status, claim.refuse.reason)
	}
	if err != nil {
		return RunView{}, err
	}
	if claim.runID != nil {
		return s.GetRun(ctx, a, *claim.runID)
	}
	s.background(func(ctx context.Context) { s.execute(s.netCtx(ctx), run.ID) })
	return s.GetRun(ctx, a, run.ID)
}

func planRunRequest(body PlanBody) RunRequest {
	opts := RunOptions{
		UpdateMode: proto.UpdateMode(body.UpdateMode), Hold: body.Hold, RebootIfRequired: body.RebootIfRequired,
		ObserveSeconds: body.ObserveSeconds, CanaryServerIDs: body.CanaryServerIDs,
		BuildCache: body.BuildCache, VSCode: body.VSCode, Journal: body.Journal, JournalSize: body.JournalSize,
	}
	if body.Kind == RunUpdate {
		approved := append([]string{}, body.Packages...)
		opts.Approved = &approved
	}
	req := RunRequest{Kind: body.Kind, GroupID: body.GroupID, ServerIDs: body.ServerIDs, Options: &opts, CanaryServerIDs: body.CanaryServerIDs, ObserveSeconds: body.ObserveSeconds, WaitAfterCanaries: body.WaitAfterCanaries, FromPlan: true}
	if body.GroupID != nil {
		// Run on the servers the person approved, not on servers added to
		// the group since.
		req.ServerIDs = body.PinnedServerIDs
	}
	return req
}

type planRefusal struct{ status, reason string }

type planClaim struct {
	row    store.FixPlan
	body   PlanBody
	runID  *int64
	refuse *planRefusal
}

// claimPlan locks the plan and its approving memberships while the run is recorded.
func (s *Service) claimPlan(ctx context.Context, tx *gorm.DB, a Actor, id int64) (planClaim, error) {
	var c planClaim
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND account_id = ?", id, a.AccountID).Take(&c.row).Error; err != nil {
		return c, mapStore(err)
	}
	row := c.row
	if row.Status == "executing" {
		if row.RunID != nil {
			c.runID = row.RunID
			return c, nil
		}
		return c, invalid("status", "the plan is already being executed")
	}
	if row.RunID != nil && (row.Status == "succeeded" || row.Status == "failed" || row.Status == "partial") {
		c.runID = row.RunID
		return c, nil
	}
	if row.Status != "approved" {
		return c, invalid("status", "the plan is not approved")
	}
	if row.ExpiresAt == nil || !row.ExpiresAt.After(s.now()) {
		c.refuse = &planRefusal{"expired", "the approval expired"}
		return c, invalid("status", "the approval has expired")
	}
	if err := row.Body.Decode(&c.body); err != nil {
		return c, err
	}
	fp, err := planFingerprint(c.body)
	if err != nil || fp != row.Fingerprint {
		c.refuse = &planRefusal{"invalidated", "fingerprint mismatch"}
		return c, invalid("status", "the plan was invalidated")
	}

	required, err := s.planApprovalRequirement(ctx, tx, a.AccountID, c.body)
	if err != nil {
		return c, err
	}
	required = max(required, row.ApprovalsRequired)
	if row.ApprovedBy == nil || (required == 2 && (row.SecondApprovedBy == nil || *row.SecondApprovedBy == *row.ApprovedBy)) {
		return c, invalid("status", "this plan needs two distinct admin approvals")
	}
	approvers := []int64{*row.ApprovedBy}
	if required == 2 {
		approvers = append(approvers, *row.SecondApprovedBy)
	}
	var active []store.Membership
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("account_id = ? AND user_id IN ? AND role IN ?", a.AccountID, approvers, []string{"owner", "admin"}).Order("user_id").Find(&active).Error; err != nil {
		return c, err
	}
	if len(active) != required {
		return c, invalid("status", "an approver is no longer an admin; propose a new plan")
	}
	target, err := s.planTarget(ctx, a.AccountID, c.body)
	if err != nil || !sameIDs(serverIDs(target), c.body.PinnedServerIDs) {
		c.refuse = &planRefusal{"invalidated", "the servers in the plan changed"}
		return c, invalid("status", "the servers changed since the plan was approved, so this approval no longer applies")
	}
	if c.body.Kind == RunUpdate {
		names, err := s.pendingNames(ctx, target, c.body.UpdateMode)
		frozen := c.body.Pending
		if frozen == nil {
			frozen = c.body.Packages
		}
		if err != nil || !sameNames(names, frozen) {
			c.refuse = &planRefusal{"invalidated", "package list changed"}
			return c, invalid("status", "the package list changed, so this approval no longer applies")
		}
	}
	return c, nil
}

// setPlanStatus records a final plan status outside any transaction.
func (s *Service) setPlanStatus(ctx context.Context, a Actor, row store.FixPlan, status, reason string) {
	res := s.db.WithContext(context.WithoutCancel(ctx)).Model(&store.FixPlan{}).Where("id = ? AND account_id = ? AND status = ? AND run_id IS NULL", row.ID, a.AccountID, "approved").
		Updates(map[string]any{"status": status, "reason": reason, "updated_at": s.now()})
	if res.Error != nil {
		s.log.Error("record plan status", "plan_id", row.ID, "status", status, "err", res.Error)
		return
	}
	if res.RowsAffected > 0 && status == "invalidated" {
		s.audit(ctx, nil, a, "plan.invalidate", row.Fingerprint, map[string]any{"id": row.ID, "reason": reason})
	}
}

// ListPlans returns plans in the actor's space.
func (s *Service) ListPlans(ctx context.Context, a Actor) ([]PlanView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil && !(a.Role.AtLeast(RoleViewer) && a.hasScope(ScopeLogs)) {
		return nil, ErrForbidden
	}
	var rows []store.FixPlan
	if err := s.db.WithContext(ctx).Where("account_id = ?", a.AccountID).Order("id desc").Limit(100).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]PlanView, 0, len(rows))
	for _, row := range rows {
		var body PlanBody
		_ = row.Body.Decode(&body)
		out = append(out, toPlan(row, body))
	}
	return out, nil
}

// GetPlan returns one plan in the actor's space.
func (s *Service) GetPlan(ctx context.Context, a Actor, id int64) (PlanView, error) {
	if err := a.can(RoleViewer, ScopeRead); err != nil && !(a.Role.AtLeast(RoleViewer) && (a.hasScope(ScopeLogs) || a.hasScope(ScopeActions))) {
		return PlanView{}, ErrForbidden
	}
	row, body, err := s.loadPlan(ctx, a, id)
	if err != nil {
		return PlanView{}, err
	}
	return toPlan(row, body), nil
}

func (s *Service) loadPlan(ctx context.Context, a Actor, id int64) (store.FixPlan, PlanBody, error) {
	var row store.FixPlan
	if err := s.db.WithContext(ctx).Where("id = ? AND account_id = ?", id, a.AccountID).Take(&row).Error; err != nil {
		return row, PlanBody{}, mapStore(err)
	}
	var body PlanBody
	if err := row.Body.Decode(&body); err != nil {
		return row, PlanBody{}, err
	}
	return row, body, nil
}

func (s *Service) invalidate(ctx context.Context, a Actor, row store.FixPlan, reason string) error {
	_ = s.db.WithContext(ctx).Model(&row).Updates(map[string]any{"status": "invalidated", "reason": reason, "updated_at": s.now()})
	s.audit(ctx, nil, a, "plan.invalidate", row.Fingerprint, map[string]any{"id": row.ID})
	return invalid("status", "%s", reason)
}

func (s *Service) needsPlan(ctx context.Context, group *store.Group, servers []store.Server) bool {
	if group != nil && group.PlanRequired {
		return true
	}
	var ids []int64
	for _, srv := range servers {
		if srv.GroupID != nil {
			ids = append(ids, *srv.GroupID)
		}
	}
	if len(ids) == 0 {
		return false
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&store.Group{}).Where("id IN ? AND plan_required", ids).Count(&n).Error; err != nil {
		return true
	}
	return n > 0
}

// planTarget loads the servers a plan covers now: the group's servers, or
// the listed servers, in the account.
func (s *Service) planTarget(ctx context.Context, accountID int64, body PlanBody) ([]store.Server, error) {
	q := s.db.WithContext(ctx).Where("account_id = ?", accountID)
	if body.GroupID != nil {
		q = q.Where("group_id = ?", *body.GroupID)
	} else {
		q = q.Where("id IN ?", body.ServerIDs)
	}
	var servers []store.Server
	if err := q.Order("id").Find(&servers).Error; err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return nil, invalid("serverIds", "no servers")
	}
	if body.GroupID == nil && len(servers) != len(uniqueIDs(body.ServerIDs)) {
		return nil, ErrNotFound
	}
	return servers, nil
}

// pendingNames is the pending package list the update would install.
func (s *Service) pendingNames(ctx context.Context, servers []store.Server, mode string) ([]string, error) {
	if mode == "" {
		mode = "security"
	}
	var names []string
	for _, srv := range servers {
		var state store.ServerState
		if err := s.db.WithContext(ctx).Where("server_id = ?", srv.ID).Take(&state).Error; err != nil || len(state.Updates) == 0 {
			return nil, invalid("packages", "the package list on %s is not known yet", srv.Name)
		}
		var upd proto.PendingUpdates
		if err := state.Updates.Decode(&upd); err != nil {
			return nil, invalid("packages", "the package list on %s is not known yet", srv.Name)
		}
		for _, p := range upd.Packages {
			if mode == "all" || p.Security {
				names = append(names, p.Name)
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names), nil
}

func serverIDs(servers []store.Server) []int64 {
	out := make([]int64, 0, len(servers))
	for _, srv := range servers {
		out = append(out, srv.ID)
	}
	return out
}

func sameIDs(a, b []int64) bool {
	aa := append([]int64(nil), a...)
	bb := append([]int64(nil), b...)
	slices.Sort(aa)
	slices.Sort(bb)
	return slices.Equal(aa, bb)
}

func sameNames(a, b []string) bool {
	aa := append([]string(nil), a...)
	bb := append([]string(nil), b...)
	slices.Sort(aa)
	slices.Sort(bb)
	return slices.Equal(aa, bb)
}

func toPlan(row store.FixPlan, body PlanBody) PlanView {
	count := 0
	if row.ApprovedBy != nil {
		count++
	}
	if row.SecondApprovedBy != nil {
		count++
	}
	return PlanView{
		Approvals: count, ApprovalsRequired: max(1, row.ApprovalsRequired),
		ID: row.ID, Status: row.Status, Fingerprint: row.Fingerprint, Body: body,
		ExpiresAt: row.ExpiresAt, RunID: row.RunID, Reason: row.Reason, CreatedAt: row.CreatedAt,
	}
}

func modeWord(mode string) string {
	if mode == "all" {
		return "package"
	}
	return "security"
}

// choosePackages checks a requested subset against the pending list. No
// request approves every pending package.
func choosePackages(requested, pending []string) ([]string, error) {
	if len(requested) == 0 {
		return pending, nil
	}
	var out []string
	for _, name := range cleanList(requested) {
		if !slices.Contains(pending, name) {
			return nil, invalid("packages", "%q is not pending on these servers", name)
		}
		out = append(out, name)
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// CancelPlan withdraws a plan that has not started. A started plan is
// stopped at its run's canary gate or not at all: cancel never touches a
// server.
func (s *Service) CancelPlan(ctx context.Context, a Actor, id int64, reason string) (PlanView, error) {
	if err := a.can(RoleAdmin, ScopeActions); err != nil {
		return PlanView{}, err
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 200 {
		return PlanView{}, invalid("reason", "must be at most 200 characters")
	}
	row, body, err := s.loadPlan(ctx, a, id)
	if err != nil {
		return PlanView{}, err
	}
	res := s.db.WithContext(ctx).Model(&store.FixPlan{}).
		Where("id = ? AND account_id = ? AND status IN ? AND run_id IS NULL", id, a.AccountID, []string{"submitted", "approved"}).
		Updates(map[string]any{"status": "cancelled", "reason": reason, "updated_at": s.now()})
	if res.Error != nil {
		return PlanView{}, res.Error
	}
	if res.RowsAffected == 0 {
		return PlanView{}, invalid("status", "only a plan that has not started can be cancelled")
	}
	s.audit(ctx, nil, a, "plan.cancel", row.Fingerprint, map[string]any{"id": row.ID, "reason": reason})
	row.Status, row.Reason = "cancelled", reason
	return toPlan(row, body), nil
}

func (s *Service) planApprovalRequirement(ctx context.Context, db *gorm.DB, accountID int64, body PlanBody) (int, error) {
	var groups []store.Group
	q := db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("account_id = ?", accountID)
	if body.GroupID != nil {
		q = q.Where("id = ?", *body.GroupID)
	} else {
		q = q.Where("id IN (SELECT group_id FROM servers WHERE account_id = ? AND id IN ?)", accountID, body.PinnedServerIDs)
	}
	if err := q.Order("id").Find(&groups).Error; err != nil {
		return 0, err
	}
	for _, group := range groups {
		if group.TwoApprovers {
			return 2, nil
		}
	}
	return 1, nil
}

// rejectSubmitted rejects the plan only while it is still submitted, so an
// approval that landed after the plan was read is not overwritten.
func (s *Service) rejectSubmitted(ctx context.Context, id int64, reason string) (bool, error) {
	res := s.db.WithContext(ctx).Model(&store.FixPlan{}).Where("id = ? AND status = ?", id, "submitted").
		Updates(map[string]any{"status": "rejected", "reason": reason, "updated_at": s.now()})
	return res.RowsAffected == 1, res.Error
}
