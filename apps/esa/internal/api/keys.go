package api

import (
	"context"
	"time"

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
	reason := ""
	if req.Body != nil {
		reason = deref(req.Body.Reason)
	}
	if err := h.fleet.ApproveKeyBaseline(ctx, a, req.ServerId, reason); err != nil {
		return nil, err
	}
	return ApproveKeyBaseline204Response{}, nil
}

func (h *Handlers) Offboarding(ctx context.Context, req OffboardingRequestObject) (OffboardingResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rep, err := h.fleet.Offboarding(ctx, a, req.Params.Label)
	if err != nil {
		return nil, err
	}
	out := Offboarding200JSONResponse{
		Statement: rep.Statement, Items: mapSlice(rep.Keys, toKey), Identity: toIdentity(rep.Identity),
		GeneratedAt: rep.GeneratedAt, Complete: rep.Complete, Partial: rep.Partial, SharedKeys: nonNil(rep.SharedKeys),
		Markdown: rep.Markdown(),
	}
	for _, g := range rep.Grants {
		out.Grants = append(out.Grants, struct {
			Fingerprint string    `json:"fingerprint"`
			From        string    `json:"from"`
			HasCommand  bool      `json:"hasCommand"`
			KeyType     string    `json:"keyType"`
			ObservedAt  time.Time `json:"observedAt"`
			Server      string    `json:"server"`
			ServerId    int64     `json:"serverId"`
			Unchecked   string    `json:"unchecked"`
			User        string    `json:"user"`
		}{g.Fingerprint, g.From, g.HasCommand, g.KeyType, g.ObservedAt, g.Server, g.ServerID, g.Unchecked, g.User})
	}
	for _, g := range rep.Unchecked {
		out.Unchecked = append(out.Unchecked, struct {
			Reason   string `json:"reason"`
			Server   string `json:"server"`
			ServerId int64  `json:"serverId"`
		}{g.Reason, g.Server, g.ServerID})
	}
	out.Grants, out.Unchecked = nonNil(out.Grants), nonNil(out.Unchecked)
	return out, nil
}

func toIdentity(v fleet.IdentityView) Identity {
	return Identity{Name: v.Name, Kind: IdentityKind(v.Kind), Status: IdentityStatus(v.Status), Note: v.Note, Keys: v.Keys, Grants: v.Grants, Servers: v.Servers}
}

func (h *Handlers) ListIdentities(ctx context.Context, _ ListIdentitiesRequestObject) (ListIdentitiesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.fleet.ListIdentities(ctx, a)
	if err != nil {
		return nil, err
	}
	return ListIdentities200JSONResponse{Items: mapSlice(items, toIdentity)}, nil
}

func (h *Handlers) SaveIdentity(ctx context.Context, req SaveIdentityRequestObject) (SaveIdentityResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "name", Message: "is required"}
	}
	v, err := h.fleet.SaveIdentity(ctx, a, fleet.IdentityView{Name: req.Body.Name, Kind: string(req.Body.Kind), Status: string(req.Body.Status), Note: deref(req.Body.Note)})
	if err != nil {
		return nil, err
	}
	return SaveIdentity200JSONResponse(toIdentity(v)), nil
}

func (h *Handlers) ListBaselineVersions(ctx context.Context, req ListBaselineVersionsRequestObject) (ListBaselineVersionsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.fleet.BaselineVersions(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	return ListBaselineVersions200JSONResponse{Items: mapSlice(items, func(v fleet.BaselineVersionView) BaselineVersion {
		return BaselineVersion{Version: v.Version, Keys: v.Keys, Reason: v.Reason, ApprovedAt: v.ApprovedAt, ApprovedBy: v.ApprovedBy}
	})}, nil
}

func toKey(v fleet.KeyView) Key {
	uses := make([]KeyOccurrence, 0, len(v.Uses))
	for _, u := range v.Uses {
		uses = append(uses, KeyOccurrence{
			ServerId: u.ServerID, ServerName: u.ServerName, User: u.User,
			Comment: u.Comment, From: u.From, HasCommand: u.HasCommand, ObservedAt: u.ObservedAt,
		})
	}
	return Key{Fingerprint: v.Fingerprint, KeyType: v.KeyType, HasCommand: v.HasCommand, Label: v.Label, Uses: uses}
}
