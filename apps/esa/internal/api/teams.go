package api

import (
	"context"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

// ---- Teams ----------------------------------------------------------------------

func toTeam(t fleet.TeamView) Team {
	return Team{Id: t.ID, Name: t.Name, Personal: t.Personal, Role: Role(t.Role), MemberCount: t.MemberCount, CreatedAt: t.CreatedAt}
}

func toMember(m fleet.MemberView) Member {
	return Member{UserId: m.UserID, Name: m.Name, Email: m.Email, Role: Role(m.Role), JoinedAt: m.JoinedAt, You: m.You}
}

func toInvite(i fleet.InviteView) Invite {
	return Invite{Id: i.ID, Role: Role(i.Role), Email: i.Email, CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt, CreatedBy: i.CreatedBy}
}

func (h *Handlers) CreateTeam(ctx context.Context, req CreateTeamRequestObject) (CreateTeamResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "name", Message: "is required"}
	}
	m, err := h.fleet.CreateTeam(ctx, a, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return CreateTeam201JSONResponse(toMembership(m)), nil
}

func (h *Handlers) GetTeam(ctx context.Context, _ GetTeamRequestObject) (GetTeamResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	t, err := h.fleet.GetTeam(ctx, a)
	if err != nil {
		return nil, err
	}
	return GetTeam200JSONResponse(toTeam(t)), nil
}

func (h *Handlers) UpdateTeam(ctx context.Context, req UpdateTeamRequestObject) (UpdateTeamResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "name", Message: "is required"}
	}
	t, err := h.fleet.RenameTeam(ctx, a, req.Body.Name)
	if err != nil {
		return nil, err
	}
	return UpdateTeam200JSONResponse(toTeam(t)), nil
}

func (h *Handlers) DeleteTeam(ctx context.Context, req DeleteTeamRequestObject) (DeleteTeamResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.DeleteTeam(ctx, a, req.Params.Confirm); err != nil {
		return nil, err
	}
	return DeleteTeam204Response{}, nil
}

// ---- Members --------------------------------------------------------------------

func (h *Handlers) ListMembers(ctx context.Context, _ ListMembersRequestObject) (ListMembersResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ms, err := h.fleet.ListMembers(ctx, a)
	if err != nil {
		return nil, err
	}
	return ListMembers200JSONResponse{Items: mapSlice(ms, toMember)}, nil
}

func (h *Handlers) UpdateMember(ctx context.Context, req UpdateMemberRequestObject) (UpdateMemberResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "role", Message: "is required"}
	}
	m, err := h.fleet.UpdateMemberRole(ctx, a, req.UserId, fleet.Role(req.Body.Role))
	if err != nil {
		return nil, err
	}
	return UpdateMember200JSONResponse(toMember(m)), nil
}

func (h *Handlers) RemoveMember(ctx context.Context, req RemoveMemberRequestObject) (RemoveMemberResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.RemoveMember(ctx, a, req.UserId); err != nil {
		return nil, err
	}
	return RemoveMember204Response{}, nil
}

// ---- Invites --------------------------------------------------------------------

func (h *Handlers) ListInvites(ctx context.Context, _ ListInvitesRequestObject) (ListInvitesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	is, err := h.fleet.ListInvites(ctx, a)
	if err != nil {
		return nil, err
	}
	return ListInvites200JSONResponse{Items: mapSlice(is, toInvite)}, nil
}

func (h *Handlers) CreateInvite(ctx context.Context, req CreateInviteRequestObject) (CreateInviteResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "role", Message: "is required"}
	}
	email := ""
	if req.Body.Email != nil {
		email = *req.Body.Email
	}
	inv, url, err := h.fleet.CreateInvite(ctx, a, fleet.Role(req.Body.Role), email)
	if err != nil {
		return nil, err
	}
	return CreateInvite201JSONResponse{Invite: toInvite(inv), Url: url}, nil
}

func (h *Handlers) RevokeInvite(ctx context.Context, req RevokeInviteRequestObject) (RevokeInviteResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.RevokeInvite(ctx, a, req.InviteId); err != nil {
		return nil, err
	}
	return RevokeInvite204Response{}, nil
}

func (h *Handlers) GetInvite(ctx context.Context, req GetInviteRequestObject) (GetInviteResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	p, err := h.fleet.PreviewInvite(ctx, a, req.Token)
	if err != nil {
		return nil, err
	}
	return GetInvite200JSONResponse{TeamName: p.TeamName, Role: Role(p.Role), InvitedBy: p.InvitedBy, ExpiresAt: p.ExpiresAt, AlreadyMember: p.AlreadyMember}, nil
}

// AcceptInvite joins the team and switches this browser session to it.
func (h *Handlers) AcceptInvite(ctx context.Context, req AcceptInviteRequestObject) (AcceptInviteResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	m, _, err := h.fleet.AcceptInvite(ctx, a, req.Token)
	if err != nil {
		return nil, err
	}
	to, err := h.sessions.SwitchAccount(ctx, m.Account.ID)
	if err != nil {
		return nil, err
	}
	return AcceptInvite200JSONResponse(toMembership(to)), nil
}
