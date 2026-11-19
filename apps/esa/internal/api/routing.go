package api

import (
	"context"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

func toRouting(r fleet.Routing) Routing {
	return Routing{Warnings: RoutingWarnings(r.Warnings), Hour: r.Hour, Timezone: r.Timezone}
}

func (h *Handlers) GetRouting(ctx context.Context, _ GetRoutingRequestObject) (GetRoutingResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	r, err := h.fleet.GetRouting(ctx, a)
	if err != nil {
		return nil, err
	}
	return GetRouting200JSONResponse(toRouting(r)), nil
}

func (h *Handlers) SetRouting(ctx context.Context, req SetRoutingRequestObject) (SetRoutingResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "warnings", Message: "is required"}
	}
	r, err := h.fleet.SetRouting(ctx, a, fleet.Routing{Warnings: string(req.Body.Warnings), Hour: req.Body.Hour, Timezone: req.Body.Timezone})
	if err != nil {
		return nil, err
	}
	return SetRouting200JSONResponse(toRouting(r)), nil
}

func (h *Handlers) ListDeliveries(ctx context.Context, req ListDeliveriesRequestObject) (ListDeliveriesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.fleet.ListDeliveries(ctx, a, deref(req.Params.Failed))
	if err != nil {
		return nil, err
	}
	return ListDeliveries200JSONResponse{Items: mapSlice(items, func(d fleet.DeliveryView) Delivery {
		return Delivery{Id: d.ID, Channel: d.Channel, Title: d.Title, Severity: d.Severity, State: DeliveryState(d.State),
			Attempts: d.Attempts, LastError: d.LastError, CreatedAt: d.CreatedAt, SentAt: d.SentAt}
	})}, nil
}

func (h *Handlers) RetryDelivery(ctx context.Context, req RetryDeliveryRequestObject) (RetryDeliveryResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.RetryDelivery(ctx, a, req.DeliveryId); err != nil {
		return nil, err
	}
	return RetryDelivery204Response{}, nil
}
