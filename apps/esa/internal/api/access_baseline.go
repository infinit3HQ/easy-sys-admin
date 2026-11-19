package api

import (
	"context"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

func (h *Handlers) ApproveAccessBaseline(ctx context.Context, req ApproveAccessBaselineRequestObject) (ApproveAccessBaselineResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "reason", Message: "is required"}
	}
	if err := h.fleet.ApproveAccessBaseline(ctx, a, req.ServerId, req.Body.Reason); err != nil {
		return nil, err
	}
	return ApproveAccessBaseline204Response{}, nil
}
