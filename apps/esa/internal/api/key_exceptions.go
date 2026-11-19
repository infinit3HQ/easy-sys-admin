package api

import (
	"context"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

func (h *Handlers) ListKeyExceptions(ctx context.Context, req ListKeyExceptionsRequestObject) (ListKeyExceptionsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.fleet.ListKeyExceptions(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	out := make([]KeyException, 0, len(items))
	for _, e := range items {
		out = append(out, KeyException{Id: e.ID, ServerId: e.ServerID, User: e.User, Fingerprint: e.Fingerprint, Until: e.Until, Reason: e.Reason})
	}
	return ListKeyExceptions200JSONResponse{Items: out}, nil
}

func (h *Handlers) CreateKeyException(ctx context.Context, req CreateKeyExceptionRequestObject) (CreateKeyExceptionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "fingerprint", Message: "is required"}
	}
	e, err := h.fleet.CreateKeyException(ctx, a, req.ServerId, req.Body.User, req.Body.Fingerprint, req.Body.Reason, req.Body.Until)
	if err != nil {
		return nil, err
	}
	return CreateKeyException201JSONResponse{Id: e.ID, ServerId: e.ServerID, User: e.User, Fingerprint: e.Fingerprint, Until: e.Until, Reason: e.Reason}, nil
}

func (h *Handlers) DeleteKeyException(ctx context.Context, req DeleteKeyExceptionRequestObject) (DeleteKeyExceptionResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.DeleteKeyException(ctx, a, req.ServerId, req.ExceptionId); err != nil {
		return nil, err
	}
	return DeleteKeyException204Response{}, nil
}
