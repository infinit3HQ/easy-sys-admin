package api

import (
	"context"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

func (h *Handlers) GetReachability(ctx context.Context, req GetReachabilityRequestObject) (GetReachabilityResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	views, err := h.fleet.GetReachability(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	items := make([]ProbeObservation, 0, len(views))
	for _, v := range views {
		items = append(items, ProbeObservation{Source: v.Source, Host: v.Host, Port: v.Port, State: ProbeObservationState(v.State), Reason: v.Reason, AttemptedAt: v.AttemptedAt, Stale: v.Stale, LastKnownState: optional(v.LastKnownState), LastKnownAt: v.LastKnownAt})
	}
	return GetReachability200JSONResponse{Items: items}, nil
}
func (h *Handlers) SubmitReachability(ctx context.Context, req SubmitReachabilityRequestObject) (SubmitReachabilityResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "report", Message: "is required"}
	}
	b := req.Body
	err = h.fleet.SubmitReachability(ctx, a, req.ServerId, fleet.ProbeInput{Source: b.Source, Host: b.Host, Port: b.Port, State: string(b.State), Reason: string(b.Reason), ObservedAt: b.ObservedAt})
	if err != nil {
		return nil, err
	}
	return SubmitReachability204Response{}, nil
}
