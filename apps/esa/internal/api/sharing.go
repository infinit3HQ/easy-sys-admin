package api

import (
	"context"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

// ---- Group sharing ----------------------------------------------------------------

// groupAccess maps fleet access to the wire enum. Views built outside the
// visibility helpers are always the caller's own.
func groupAccess(a fleet.Access) GroupAccess {
	switch a {
	case fleet.AccessView:
		return GroupAccessView
	case fleet.AccessOperate:
		return GroupAccessOperate
	}
	return GroupAccessOwn
}

func toGroupShare(v fleet.GroupShareView) GroupShare {
	return GroupShare{
		Id: v.ID, GroupId: v.GroupID, GroupName: v.GroupName, OwnerTeam: v.OwnerTeam, Team: v.Team,
		Access: ShareAccess(v.Access), CreatedAt: v.CreatedAt,
	}
}

func toShareLink(v fleet.ShareLinkView) ShareLink {
	return ShareLink{Id: v.ID, Access: ShareAccess(v.Access), CreatedAt: v.CreatedAt, ExpiresAt: v.ExpiresAt, CreatedBy: v.CreatedBy}
}

func toSharePreview(p fleet.SharePreview) SharePreview {
	return SharePreview{
		GroupName: p.GroupName, OwnerTeam: p.OwnerTeam, Access: ShareAccess(p.Access), ServerCount: p.ServerCount,
		ExpiresAt: p.ExpiresAt, Teams: mapSlice(p.Teams, toMembership),
	}
}

func (h *Handlers) ListGroupShares(ctx context.Context, req ListGroupSharesRequestObject) (ListGroupSharesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	shares, err := h.fleet.ListGroupShares(ctx, a, req.GroupId)
	if err != nil {
		return nil, err
	}
	return ListGroupShares200JSONResponse{Items: mapSlice(shares, toGroupShare)}, nil
}

func (h *Handlers) ListShareLinks(ctx context.Context, req ListShareLinksRequestObject) (ListShareLinksResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	links, err := h.fleet.ListShareLinks(ctx, a, req.GroupId)
	if err != nil {
		return nil, err
	}
	return ListShareLinks200JSONResponse{Items: mapSlice(links, toShareLink)}, nil
}

func (h *Handlers) CreateShareLink(ctx context.Context, req CreateShareLinkRequestObject) (CreateShareLinkResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "access", Message: "is required"}
	}
	created, err := h.fleet.CreateShareLink(ctx, a, req.GroupId, string(req.Body.Access))
	if err != nil {
		return nil, err
	}
	return CreateShareLink201JSONResponse{Link: toShareLink(created.Link), Url: created.URL}, nil
}

func (h *Handlers) RevokeShareLink(ctx context.Context, req RevokeShareLinkRequestObject) (RevokeShareLinkResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.RevokeShareLink(ctx, a, req.LinkId); err != nil {
		return nil, err
	}
	return RevokeShareLink204Response{}, nil
}

func (h *Handlers) UpdateGroupShare(ctx context.Context, req UpdateGroupShareRequestObject) (UpdateGroupShareResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "access", Message: "is required"}
	}
	sh, err := h.fleet.UpdateGroupShare(ctx, a, req.ShareId, string(req.Body.Access))
	if err != nil {
		return nil, err
	}
	return UpdateGroupShare200JSONResponse(toGroupShare(sh)), nil
}

func (h *Handlers) RemoveGroupShare(ctx context.Context, req RemoveGroupShareRequestObject) (RemoveGroupShareResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.RemoveGroupShare(ctx, a, req.ShareId); err != nil {
		return nil, err
	}
	return RemoveGroupShare204Response{}, nil
}

func (h *Handlers) GetShareLink(ctx context.Context, req GetShareLinkRequestObject) (GetShareLinkResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	p, err := h.fleet.PreviewShareLink(ctx, a, req.Token)
	if err != nil {
		return nil, err
	}
	return GetShareLink200JSONResponse(toSharePreview(p)), nil
}

func (h *Handlers) AcceptShareLink(ctx context.Context, req AcceptShareLinkRequestObject) (AcceptShareLinkResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil || req.Body.TeamId <= 0 {
		return nil, &fleet.ValidationError{Field: "teamId", Message: "is required"}
	}
	sh, err := h.fleet.AcceptShareLink(ctx, a, req.Token, req.Body.TeamId)
	if err != nil {
		return nil, err
	}
	return AcceptShareLink200JSONResponse(toGroupShare(sh)), nil
}
