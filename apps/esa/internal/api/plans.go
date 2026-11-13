package api

import (
	"context"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

func fleetPlan(b PlanBody) fleet.PlanBody {
	body := fleet.PlanBody{
		Kind: fleet.RunKind(b.Kind), GroupID: b.GroupId,
		UpdateMode: string(deref(b.UpdateMode)), JournalSize: deref(b.JournalSize),
		RebootIfRequired: deref(b.RebootIfRequired), ObserveSeconds: deref(b.ObserveSeconds),
		BuildCache: deref(b.BuildCache), VSCode: deref(b.Vscode), Journal: deref(b.Journal),
	}
	if b.ServerIds != nil {
		body.ServerIDs = *b.ServerIds
	}
	if b.Hold != nil {
		body.Hold = *b.Hold
	}
	if b.CanaryServerIds != nil {
		body.CanaryServerIDs = *b.CanaryServerIds
	}
	if b.Packages != nil {
		body.Packages = *b.Packages
	}
	body.WaitAfterCanaries = deref(b.WaitAfterCanaries)
	return body
}

func toPlan(v fleet.PlanView) FixPlan {
	body := PlanBody{Kind: PlanBodyKind(v.Body.Kind)}
	if v.Body.GroupID != nil {
		body.GroupId = v.Body.GroupID
	}
	if len(v.Body.ServerIDs) > 0 {
		ids := append([]int64(nil), v.Body.ServerIDs...)
		body.ServerIds = &ids
	}
	if v.Body.UpdateMode != "" {
		mode := PlanBodyUpdateMode(v.Body.UpdateMode)
		body.UpdateMode = &mode
	}
	if len(v.Body.Hold) > 0 {
		hold := append([]string(nil), v.Body.Hold...)
		body.Hold = &hold
	}
	if v.Body.RebootIfRequired {
		v := true
		body.RebootIfRequired = &v
	}
	if v.Body.ObserveSeconds != 0 {
		n := v.Body.ObserveSeconds
		body.ObserveSeconds = &n
	}
	if len(v.Body.CanaryServerIDs) > 0 {
		ids := append([]int64(nil), v.Body.CanaryServerIDs...)
		body.CanaryServerIds = &ids
	}
	if v.Body.JournalSize != "" {
		size := v.Body.JournalSize
		body.JournalSize = &size
	}
	if len(v.Body.Packages) > 0 {
		pkgs := append([]string(nil), v.Body.Packages...)
		body.Packages = &pkgs
	}
	if len(v.Body.Pending) > 0 {
		pending := append([]string(nil), v.Body.Pending...)
		body.Pending = &pending
	}
	if v.Body.WaitAfterCanaries {
		wait := true
		body.WaitAfterCanaries = &wait
	}
	if len(v.Body.PinnedServerIDs) > 0 {
		ids := append([]int64(nil), v.Body.PinnedServerIDs...)
		body.PinnedServerIds = &ids
	}
	// A person approves what they see, so every cleanup switch is shown,
	// off as well as on.
	if v.Body.Kind == fleet.RunClean || v.Body.Kind == fleet.RunMeasure {
		buildCache, vscode, journal := v.Body.BuildCache, v.Body.VSCode, v.Body.Journal
		body.BuildCache, body.Vscode, body.Journal = &buildCache, &vscode, &journal
	}
	return FixPlan{
		Id: v.ID, Status: FixPlanStatus(v.Status), Fingerprint: v.Fingerprint, Body: body,
		ExpiresAt: v.ExpiresAt, RunId: v.RunID, Reason: v.Reason, CreatedAt: v.CreatedAt,
	}
}

func (h *Handlers) ListPlans(ctx context.Context, _ ListPlansRequestObject) (ListPlansResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.fleet.ListPlans(ctx, a)
	if err != nil {
		return nil, err
	}
	return ListPlans200JSONResponse{Items: mapSlice(items, toPlan)}, nil
}

func (h *Handlers) GetPlan(ctx context.Context, req GetPlanRequestObject) (GetPlanResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.fleet.GetPlan(ctx, a, req.PlanId)
	if err != nil {
		return nil, err
	}
	return GetPlan200JSONResponse(toPlan(v)), nil
}

func (h *Handlers) ProposePlan(ctx context.Context, req ProposePlanRequestObject) (ProposePlanResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "kind", Message: "is required"}
	}
	v, err := h.fleet.ProposePlan(ctx, a, fleetPlan(*req.Body))
	if err != nil {
		return nil, err
	}
	return ProposePlan201JSONResponse(toPlan(v)), nil
}

func (h *Handlers) ApprovePlan(ctx context.Context, req ApprovePlanRequestObject) (ApprovePlanResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.fleet.ApprovePlan(ctx, a, req.PlanId)
	if err != nil {
		return nil, err
	}
	return ApprovePlan200JSONResponse(toPlan(v)), nil
}

func (h *Handlers) RejectPlan(ctx context.Context, req RejectPlanRequestObject) (RejectPlanResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "reason", Message: "is required"}
	}
	v, err := h.fleet.RejectPlan(ctx, a, req.PlanId, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return RejectPlan200JSONResponse(toPlan(v)), nil
}

func (h *Handlers) ExecutePlan(ctx context.Context, req ExecutePlanRequestObject) (ExecutePlanResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	run, err := h.fleet.ExecutePlan(ctx, a, req.PlanId)
	if err != nil {
		return nil, err
	}
	return ExecutePlan202JSONResponse(toRun(run)), nil
}

func (h *Handlers) CancelPlan(ctx context.Context, req CancelPlanRequestObject) (CancelPlanResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	reason := ""
	if req.Body != nil {
		reason = deref(req.Body.Reason)
	}
	v, err := h.fleet.CancelPlan(ctx, a, req.PlanId, reason)
	if err != nil {
		return nil, err
	}
	return CancelPlan200JSONResponse(toPlan(v)), nil
}
