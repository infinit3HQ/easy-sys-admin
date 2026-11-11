package api

import (
	"context"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

func (h *Handlers) ListKeys(ctx context.Context, _ ListKeysRequestObject) (ListKeysResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.fleet.ListKeys(ctx, a)
	if err != nil {
		return nil, err
	}
	return ListKeys200JSONResponse{Items: mapSlice(items, toKey)}, nil
}

func (h *Handlers) LabelKey(ctx context.Context, req LabelKeyRequestObject) (LabelKeyResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "fingerprint", Message: "is required"}
	}
	if err := h.fleet.LabelKey(ctx, a, req.Body.Fingerprint, req.Body.Label); err != nil {
		return nil, err
	}
	return LabelKey204Response{}, nil
}

func (h *Handlers) ApproveKeyBaseline(ctx context.Context, req ApproveKeyBaselineRequestObject) (ApproveKeyBaselineResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.ApproveKeyBaseline(ctx, a, req.ServerId); err != nil {
		return nil, err
	}
	return ApproveKeyBaseline204Response{}, nil
}

func (h *Handlers) Offboarding(ctx context.Context, req OffboardingRequestObject) (OffboardingResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	statement, items, err := h.fleet.Offboarding(ctx, a, req.Params.Label)
	if err != nil {
		return nil, err
	}
	return Offboarding200JSONResponse{Statement: statement, Items: mapSlice(items, toKey)}, nil
}

func toKey(v fleet.KeyView) Key {
	uses := make([]KeyOccurrence, 0, len(v.Uses))
	for _, u := range v.Uses {
		uses = append(uses, KeyOccurrence{
			ServerId: u.ServerID, ServerName: u.ServerName, User: u.User,
			Comment: u.Comment, From: u.From, ObservedAt: u.ObservedAt,
		})
	}
	return Key{Fingerprint: v.Fingerprint, KeyType: v.KeyType, HasCommand: v.HasCommand, Label: v.Label, Uses: uses}
}
