package api

import (
	"context"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

func (h *Handlers) ListServices(ctx context.Context, req ListServicesRequestObject) (ListServicesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.fleet.Services(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	return ListServices200JSONResponse{Items: nonNil(items)}, nil
}

func (h *Handlers) ServiceAction(ctx context.Context, req ServiceActionRequestObject) (ServiceActionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.ServiceAction(ctx, a, req.ServerId, req.Unit, string(req.Action)); err != nil {
		return nil, err
	}
	return ServiceAction200JSONResponse{Ok: true}, nil
}

func (h *Handlers) ListContainers(ctx context.Context, req ListContainersRequestObject) (ListContainersResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.fleet.Containers(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	return ListContainers200JSONResponse{Items: nonNil(items)}, nil
}

func (h *Handlers) CheckContainerImages(ctx context.Context, req CheckContainerImagesRequestObject) (CheckContainerImagesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.fleet.ImageCheck(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	return CheckContainerImages200JSONResponse{Items: nonNil(items)}, nil
}

func (h *Handlers) ContainerAction(ctx context.Context, req ContainerActionRequestObject) (ContainerActionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	confirm := ""
	if req.Body != nil {
		confirm = deref(req.Body.Confirm)
	}
	if req.Action == "update" {
		out, err := h.fleet.ContainerUpdate(ctx, a, req.ServerId, req.Name, confirm)
		if err != nil {
			return nil, err
		}
		return ContainerAction200JSONResponse{Ok: true, Output: optional(out)}, nil
	}
	if err := h.fleet.ContainerAction(ctx, a, req.ServerId, req.Name, string(req.Action), confirm); err != nil {
		return nil, err
	}
	return ContainerAction200JSONResponse{Ok: true}, nil
}

func (h *Handlers) GetServerLogs(ctx context.Context, req GetServerLogsRequestObject) (GetServerLogsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	q := fleet.LogQuery{Unit: deref(p.Unit), Container: deref(p.Container), Grep: deref(p.Grep), Lines: 200, Priority: -1}
	if p.Lines != nil {
		q.Lines = *p.Lines
	}
	if p.Priority != nil {
		q.Priority = *p.Priority
	}
	items, err := h.fleet.Logs(ctx, a, req.ServerId, q)
	if err != nil {
		return nil, err
	}
	return GetServerLogs200JSONResponse{Items: nonNil(items)}, nil
}

func (h *Handlers) GetServerAccess(ctx context.Context, req GetServerAccessRequestObject) (GetServerAccessResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := h.fleet.Access(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	return GetServerAccess200JSONResponse(r), nil
}

func (h *Handlers) GetServerSecurity(ctx context.Context, req GetServerSecurityRequestObject) (GetServerSecurityResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := h.fleet.Security(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	return GetServerSecurity200JSONResponse(r), nil
}

func (h *Handlers) GetServerJobs(ctx context.Context, req GetServerJobsRequestObject) (GetServerJobsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := h.fleet.Jobs(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	return GetServerJobs200JSONResponse(r), nil
}

func (h *Handlers) GetServerRecovery(ctx context.Context, req GetServerRecoveryRequestObject) (GetServerRecoveryResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := h.fleet.Recovery(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	return GetServerRecovery200JSONResponse(r), nil
}
