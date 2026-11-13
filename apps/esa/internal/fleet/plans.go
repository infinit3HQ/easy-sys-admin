package fleet

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
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
	// Packages is the pending package list frozen when an update plan is
	// proposed. A different list at execution invalidates the plan.
	Packages []string `json:"packages,omitempty"`
	// PinnedServerIDs are the servers the plan covered when it was
	// proposed. A server added to or removed from the group since
	// invalidates the plan, so an approval never reaches a new server.
	PinnedServerIDs []int64 `json:"pinnedServerIds,omitempty"`
}

// PlanView is a plan as the API shows it.
type PlanView struct {
	ID          int64
	Status      string
	Fingerprint string
	Body        PlanBody
	ExpiresAt   *time.Time
	RunID       *int64
	Reason      string
	CreatedAt   time.Time
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
		body.ObserveSeconds, body.CanaryServerIDs = 0, nil
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
		body.Packages = names
		if len(names) == 0 {
			return PlanView{}, invalid("packages", "no %s updates are pending on these servers, so there is nothing to approve", modeWord(body.UpdateMode))
		}
	}
	fp, err := planFingerprint(body)
	if err != nil {
		return PlanView{}, err
	}
	now := s.now()
	row := store.FixPlan{
		AccountID: a.AccountID, Status: "submitted", Fingerprint: fp, Body: store.MustJSON(body),
		CreatedBy: a.UserID, ProposerTokenID: a.TokenID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Omit("Account").Create(&row).Error; err != nil {
		return PlanView{}, err
	}
	s.audit(ctx, nil, a, "plan.propose", fp, map[string]any{"id": row.ID, "kind": body.Kind})
	return toPlan(row, body), nil
}

// ApprovePlan records a human approval. A token cannot approve, including an admin token.
func (s *Service) ApprovePlan(ctx context.Context, a Actor, id int64) (PlanView, error) {
	if !a.isPerson() || !a.Role.AtLeast(RoleAdmin) {
		return PlanView{}, ErrForbidden
	}
	row, body, err := s.loadPlan(ctx, a, id)
	if err != nil {
		return PlanView{}, err
	}
	if row.Status != "submitted" {
		return PlanView{}, invalid("status", "only a submitted plan can be approved")
	}
	fp, err := planFingerprint(body)
	if err != nil || fp != row.Fingerprint {
		return PlanView{}, s.invalidate(ctx, a, row, "the plan body does not match its fingerprint")
	}
	now := s.now()
	exp := now.Add(planApprovalTTL)
	res := s.db.WithContext(ctx).Model(&store.FixPlan{}).
		Where("id = ? AND account_id = ? AND status = ?", row.ID, a.AccountID, "submitted").
		Updates(map[string]any{"status": "approved", "approved_by": a.UserID, "approved_at": now, "expires_at": exp, "updated_at": now})
	if res.Error != nil {
		return PlanView{}, res.Error
	}
	if res.RowsAffected == 0 {
		return PlanView{}, invalid("status", "only a submitted plan can be approved")
	}
	s.audit(ctx, nil, a, "plan.approve", row.Fingerprint, map[string]any{"id": row.ID, "expires": exp})
	row.Status, row.ExpiresAt, row.ApprovedBy, row.ApprovedAt = "approved", &exp, &a.UserID, &now
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
	now := s.now()
	if err := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{"status": "rejected", "reason": reason, "updated_at": now}).Error; err != nil {
		return PlanView{}, err
	}
	s.audit(ctx, nil, a, "plan.reject", row.Fingerprint, map[string]any{"id": row.ID})
	row.Status, row.Reason = "rejected", reason
	return toPlan(row, body), nil
}

// ExecutePlan starts the approved plan once. A later call returns the same run.
// A token that can read logs cannot execute.
//
// The plan is claimed in one transaction and the run starts after it
// commits, so a failed commit cannot leave a run with no plan pointing at
// it. A refusal (expired, invalidated) is written after the transaction,
// so returning the error does not roll the status back.
func (s *Service) ExecutePlan(ctx context.Context, a Actor, id int64) (RunView, error) {
	if err := a.can(RoleAdmin, ScopeActions); err != nil {
		return RunView{}, err
	}
	if a.hasScope(ScopeLogs) && a.Scopes != nil {
		return RunView{}, ErrForbidden
	}
	claim, err := s.claimPlan(ctx, a, id)
	if claim.refuse != nil {
		s.setPlanStatus(ctx, a, claim.row, claim.refuse.status, claim.refuse.reason)
	}
	if err != nil {
		return RunView{}, err
	}
	if claim.runID != nil {
		return s.GetRun(ctx, a, *claim.runID)
	}
	body := claim.body
	opts := RunOptions{
		UpdateMode: proto.UpdateMode(body.UpdateMode), Hold: body.Hold, RebootIfRequired: body.RebootIfRequired,
		ObserveSeconds: body.ObserveSeconds, CanaryServerIDs: body.CanaryServerIDs,
		BuildCache: body.BuildCache, VSCode: body.VSCode, Journal: body.Journal, JournalSize: body.JournalSize,
	}
	if body.Kind == RunUpdate {
		approved := append([]string{}, body.Packages...)
		opts.Approved = &approved
	}
	req := RunRequest{Kind: body.Kind, GroupID: body.GroupID, ServerIDs: body.ServerIDs, Options: &opts, CanaryServerIDs: body.CanaryServerIDs, ObserveSeconds: body.ObserveSeconds, FromPlan: true}
	if body.GroupID != nil {
		// Run on the servers the person approved, not on servers added to
		// the group since.
		req.ServerIDs = body.PinnedServerIDs
	}
	run, err := s.StartRun(ctx, a, req)
	if err != nil {
		s.setPlanStatus(ctx, a, claim.row, "failed", err.Error())
		return RunView{}, err
	}
	if err := s.db.WithContext(context.WithoutCancel(ctx)).Model(&store.FixPlan{}).Where("id = ?", claim.row.ID).
		Updates(map[string]any{"run_id": run.ID, "updated_at": s.now()}).Error; err != nil {
		s.log.Error("record plan run", "plan_id", claim.row.ID, "run_id", run.ID, "err", err)
	}
	s.audit(ctx, nil, a, "plan.execute", claim.row.Fingerprint, map[string]any{"id": claim.row.ID, "run": run.ID})
	return run, nil
}

type planRefusal struct{ status, reason string }

type planClaim struct {
	row    store.FixPlan
	body   PlanBody
	runID  *int64
	refuse *planRefusal
}

// claimPlan moves an approved plan to executing, or returns the run it
// already started. A refusal is returned for the caller to record.
func (s *Service) claimPlan(ctx context.Context, a Actor, id int64) (planClaim, error) {
	var c planClaim
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND account_id = ?", id, a.AccountID).Take(&c.row).Error; err != nil {
			return mapStore(err)
		}
		row := c.row
		if row.Status == "executing" {
			if row.RunID != nil {
				c.runID = row.RunID
				return nil
			}
			return invalid("status", "the plan is already being executed")
		}
		if row.RunID != nil && (row.Status == "succeeded" || row.Status == "failed" || row.Status == "partial") {
			c.runID = row.RunID
			return nil
		}
		if row.Status != "approved" {
			return invalid("status", "the plan is not approved")
		}
		if row.ExpiresAt == nil || !row.ExpiresAt.After(s.now()) {
			c.refuse = &planRefusal{"expired", "the approval expired"}
			return invalid("status", "the approval has expired")
		}
		if err := row.Body.Decode(&c.body); err != nil {
			return err
		}
		fp, err := planFingerprint(c.body)
		if err != nil || fp != row.Fingerprint {
			c.refuse = &planRefusal{"invalidated", "fingerprint mismatch"}
			return invalid("status", "the plan was invalidated")
		}
		target, err := s.planTarget(ctx, a.AccountID, c.body)
		if err != nil || !sameIDs(serverIDs(target), c.body.PinnedServerIDs) {
			c.refuse = &planRefusal{"invalidated", "the servers in the plan changed"}
			return invalid("status", "the servers changed since the plan was approved, so this approval no longer applies")
		}
		if c.body.Kind == RunUpdate {
			names, err := s.pendingNames(ctx, target, c.body.UpdateMode)
			if err != nil || !sameNames(names, c.body.Packages) {
				c.refuse = &planRefusal{"invalidated", "package list changed"}
				return invalid("status", "the package list changed, so this approval no longer applies")
			}
		}
		res := tx.Model(&store.FixPlan{}).Where("id = ? AND status = ? AND run_id IS NULL", row.ID, "approved").
			Updates(map[string]any{"status": "executing", "updated_at": s.now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return invalid("status", "the plan is already being executed")
		}
		return nil
	})
	return c, err
}

// setPlanStatus records a final plan status outside any transaction.
func (s *Service) setPlanStatus(ctx context.Context, a Actor, row store.FixPlan, status, reason string) {
	err := s.db.WithContext(context.WithoutCancel(ctx)).Model(&store.FixPlan{}).Where("id = ?", row.ID).
		Updates(map[string]any{"status": status, "reason": reason, "updated_at": s.now()}).Error
	if err != nil {
		s.log.Error("record plan status", "plan_id", row.ID, "status", status, "err", err)
		return
	}
	if status == "invalidated" {
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
	return PlanView{
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
