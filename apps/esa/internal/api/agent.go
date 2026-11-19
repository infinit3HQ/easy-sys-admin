package api

import (
	"context"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

func (h *Handlers) CreateAgentServer(ctx context.Context, r CreateAgentServerRequestObject) (CreateAgentServerResponseObject, error) {
	a, e := actor(ctx)
	if e != nil {
		return nil, e
	}
	if r.Body == nil {
		return nil, &fleet.ValidationError{Field: "name", Message: "is required"}
	}
	id, token, e := h.fleet.CreateAgentServer(ctx, a, r.Body.Name, r.Body.GroupId)
	if e != nil {
		return nil, e
	}
	pin, e := h.fleet.AgentSigningPin(ctx, a.AccountID, id)
	if e != nil {
		return nil, e
	}
	return CreateAgentServer201JSONResponse{ServerId: id, Token: token, SigningPin: pin}, nil
}
func (h *Handlers) RotateAgentCredential(ctx context.Context, r RotateAgentCredentialRequestObject) (RotateAgentCredentialResponseObject, error) {
	a, e := actor(ctx)
	if e != nil {
		return nil, e
	}
	token, e := h.fleet.RotateAgentCredential(ctx, a, r.ServerId)
	if e != nil {
		return nil, e
	}
	pin, e := h.fleet.AgentSigningPin(ctx, a.AccountID, r.ServerId)
	if e != nil {
		return nil, e
	}
	return RotateAgentCredential200JSONResponse{ServerId: r.ServerId, Token: token, SigningPin: pin}, nil
}
func (h *Handlers) RevokeAgentCredential(ctx context.Context, r RevokeAgentCredentialRequestObject) (RevokeAgentCredentialResponseObject, error) {
	a, e := actor(ctx)
	if e != nil {
		return nil, e
	}
	if e = h.fleet.RevokeAgentCredential(ctx, a, r.ServerId); e != nil {
		return nil, e
	}
	return RevokeAgentCredential204Response{}, nil
}
func (h *Handlers) PollAgent(ctx context.Context, r PollAgentRequestObject) (PollAgentResponseObject, error) {
	a, e := h.fleet.AuthenticateAgent(ctx, r.Params.XEsaAgentToken)
	if e != nil {
		return nil, errUnauthenticated
	}
	cmd, e := h.fleet.PollAgent(ctx, a)
	if e != nil {
		return nil, e
	}
	out := PollAgent200JSONResponse{}
	if cmd != nil {
		out.Command = &AgentCommand{Id: cmd.ID, Action: cmd.Action, Args: cmd.Args, ExpiresAt: cmd.ExpiresAt, ServerId: &cmd.ServerID, Signature: &cmd.Signature}
	}
	return out, nil
}
func (h *Handlers) SubmitAgentResult(ctx context.Context, r SubmitAgentResultRequestObject) (SubmitAgentResultResponseObject, error) {
	a, e := h.fleet.AuthenticateAgent(ctx, r.Params.XEsaAgentToken)
	if e != nil {
		return nil, errUnauthenticated
	}
	if r.Body == nil {
		return nil, &fleet.ValidationError{Field: "result", Message: "is required"}
	}
	if e = h.fleet.SubmitAgentResult(ctx, a, r.Body.Id, r.Body.Output, r.Body.Exit); e != nil {
		return nil, e
	}
	return SubmitAgentResult204Response{}, nil
}
