package api

import (
	"context"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
)

func toLogin(l *Login) *remote.LoginAuth {
	if l == nil {
		return nil
	}
	out := &remote.LoginAuth{User: l.Username}
	if l.Password != nil {
		out.Password = *l.Password
	}
	if l.PrivateKey != nil {
		out.PrivateKey = []byte(*l.PrivateKey)
	}
	if l.Passphrase != nil {
		out.Passphrase = *l.Passphrase
	}
	return out
}

func (h *Handlers) ProbeServer(ctx context.Context, req ProbeServerRequestObject) (ProbeServerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "host", Message: "is required"}
	}
	res, err := h.fleet.ProbeServer(ctx, a, req.Body.Host, deref(req.Body.Port))
	if err != nil {
		return nil, err
	}
	return ProbeServer200JSONResponse{HostKey: res.HostKey, Fingerprint: res.Fingerprint, KeyType: res.KeyType}, nil
}

func (h *Handlers) AddServer(ctx context.Context, req AddServerRequestObject) (AddServerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, &fleet.ValidationError{Field: "name", Message: "is required"}
	}
	v, err := h.fleet.AddServer(ctx, a, fleet.AddServerInput{
		Name: b.Name, Host: b.Host, Port: deref(b.Port), GroupID: b.GroupId, HostKey: b.HostKey,
		Login: *toLogin(&b.Login), KeepLogin: deref(b.KeepLogin), HostsControlPlane: deref(b.HostsControlPlane),
	})
	if err != nil {
		return nil, err
	}
	return AddServer201JSONResponse(toServer(v)), nil
}

func (h *Handlers) CreateEnrollment(ctx context.Context, req CreateEnrollmentRequestObject) (CreateEnrollmentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, &fleet.ValidationError{Field: "name", Message: "is required"}
	}
	e, err := h.fleet.CreateEnrollment(ctx, a, fleet.EnrollInput{
		Name: b.Name, Host: b.Host, Port: deref(b.Port), GroupID: b.GroupId, HostsControlPlane: deref(b.HostsControlPlane),
	})
	if err != nil {
		return nil, err
	}
	return CreateEnrollment201JSONResponse(toEnrollment(e)), nil
}

func (h *Handlers) RenewEnrollment(ctx context.Context, req RenewEnrollmentRequestObject) (RenewEnrollmentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	e, err := h.fleet.NewEnrollToken(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	return RenewEnrollment200JSONResponse(toEnrollment(e)), nil
}

func (h *Handlers) GetServer(ctx context.Context, req GetServerRequestObject) (GetServerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.fleet.GetServer(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	return GetServer200JSONResponse(toServerDetail(v)), nil
}

func (h *Handlers) UpdateServer(ctx context.Context, req UpdateServerRequestObject) (UpdateServerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		b = &UpdateServerRequest{}
	}
	v, err := h.fleet.UpdateServer(ctx, a, req.ServerId, fleet.UpdateServerInput{
		Name: b.Name, GroupID: b.GroupId, ClearGroup: deref(b.ClearGroup),
		HostsControlPlane: b.HostsControlPlane, ProtectedContainers: b.ProtectedContainers, Note: b.Note,
	})
	if err != nil {
		return nil, err
	}
	return UpdateServer200JSONResponse(toServer(v)), nil
}

func (h *Handlers) DeleteServer(ctx context.Context, req DeleteServerRequestObject) (DeleteServerResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.DeleteServer(ctx, a, req.ServerId, deref(req.Params.Uninstall)); err != nil {
		return nil, err
	}
	return DeleteServer204Response{}, nil
}

func (h *Handlers) AcceptHostKey(ctx context.Context, req AcceptHostKeyRequestObject) (AcceptHostKeyResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "fingerprint", Message: "is required"}
	}
	v, err := h.fleet.AcceptHostKey(ctx, a, req.ServerId, req.Body.Fingerprint)
	if err != nil {
		return nil, err
	}
	return AcceptHostKey200JSONResponse(toServer(v)), nil
}

func (h *Handlers) ReinstallHelper(ctx context.Context, req ReinstallHelperRequestObject) (ReinstallHelperResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	var login *remote.LoginAuth
	if req.Body != nil {
		login = toLogin(req.Body.Login)
	}
	v, err := h.fleet.ReinstallHelper(ctx, a, req.ServerId, login)
	if err != nil {
		return nil, err
	}
	return ReinstallHelper200JSONResponse(toServer(v)), nil
}

var windows = map[GetServerMetricsParamsWindow]time.Duration{
	"1h": time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour, "90d": 90 * 24 * time.Hour,
}

func (h *Handlers) GetServerMetrics(ctx context.Context, req GetServerMetricsRequestObject) (GetServerMetricsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	w := GetServerMetricsParamsWindow("24h")
	if req.Params.Window != nil {
		w = *req.Params.Window
	}
	d, ok := windows[w]
	if !ok {
		return nil, &fleet.ValidationError{Field: "window", Message: "use 1h, 24h, 7d, 30d, or 90d"}
	}
	pts, err := h.fleet.Metrics(ctx, a, req.ServerId, d)
	if err != nil {
		return nil, err
	}
	out := make([]MetricPoint, 0, len(pts))
	for _, p := range pts {
		out = append(out, MetricPoint{At: p.At, CpuPercent: p.CPUPercent, MemPercent: p.MemPercent, DiskPercent: p.DiskPercent, Load1: p.Load1})
	}
	resp := GetServerMetrics200JSONResponse{Window: string(w), Points: out}
	if days, ok, err := h.fleet.DiskForecast(ctx, a, req.ServerId); err != nil {
		return nil, err
	} else if ok {
		resp.DiskFullInDays = &days
	}
	return resp, nil
}

func (h *Handlers) CheckServerUpdates(ctx context.Context, req CheckServerUpdatesRequestObject) (CheckServerUpdatesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.fleet.CheckUpdates(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	return CheckServerUpdates200JSONResponse(toServerDetail(v)), nil
}
