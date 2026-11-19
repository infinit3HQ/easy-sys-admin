package api

import (
	"context"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

func (h *Handlers) CreateVulnException(ctx context.Context, req CreateVulnExceptionRequestObject) (CreateVulnExceptionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "advisory", Message: "is required"}
	}
	e, err := h.fleet.CreateVulnException(ctx, a, req.ServerId, req.Body.Advisory, req.Body.Package, req.Body.Reason, req.Body.Until)
	if err != nil {
		return nil, err
	}
	return CreateVulnException201JSONResponse{Id: e.ID, ServerId: e.ServerID, Advisory: e.AdvisoryID, Package: e.Package, Until: e.Until, Reason: e.Reason}, nil
}

func (h *Handlers) DeleteVulnException(ctx context.Context, req DeleteVulnExceptionRequestObject) (DeleteVulnExceptionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.DeleteVulnException(ctx, a, req.ServerId, req.ExceptionId); err != nil {
		return nil, err
	}
	return DeleteVulnException204Response{}, nil
}
