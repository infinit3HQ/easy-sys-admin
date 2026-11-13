package api

import (
	"context"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

// ---- Groups ---------------------------------------------------------------------

func toGroupInput(b *GroupInput) fleet.GroupInput {
	days := func(ds []Day) []string {
		out := make([]string, len(ds))
		for i, d := range ds {
			out[i] = string(d)
		}
		return out
	}
	return fleet.GroupInput{
		Name: b.Name, Timezone: b.Timezone,
		Clean: fleet.CleanPolicy{
			Enabled: b.Clean.Enabled, Days: days(b.Clean.Days), Time: b.Clean.Time,
			BuildCache: b.Clean.BuildCache, VSCode: b.Clean.Vscode, Journal: b.Clean.Journal, JournalSize: b.Clean.JournalSize,
		},
		Updates: fleet.UpdatePolicy{
			Enabled: b.Updates.Enabled, Mode: proto.UpdateMode(b.Updates.Mode), Days: days(b.Updates.Days), Time: b.Updates.Time,
			Hold: b.Updates.Hold, RebootIfRequired: b.Updates.RebootIfRequired,
			BatchSize: b.Updates.BatchSize, MaxFailures: b.Updates.MaxFailures, PlanRequired: b.Updates.PlanRequired,
		},
		Checks: toFleetChecks(b.Checks),
	}
}

func toFleetChecks(in *[]HealthCheck) []fleet.HealthCheck {
	if in == nil {
		return nil
	}
	out := make([]fleet.HealthCheck, len(*in))
	for i, c := range *in {
		out[i] = fleet.HealthCheck{
			Name: c.Name, Kind: string(c.Kind), Blocking: c.Blocking, Port: deref(c.Port),
			TLS: deref(c.Tls), Method: string(deref(c.Method)), Path: deref(c.Path),
			Unit: deref(c.Unit), Container: deref(c.Container),
		}
	}
	return out
}

func (h *Handlers) ListGroups(ctx context.Context, _ ListGroupsRequestObject) (ListGroupsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	gs, err := h.fleet.ListGroups(ctx, a)
	if err != nil {
		return nil, err
	}
	return ListGroups200JSONResponse{Items: mapSlice(gs, toGroup)}, nil
}

func (h *Handlers) CreateGroup(ctx context.Context, req CreateGroupRequestObject) (CreateGroupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "name", Message: "is required"}
	}
	g, err := h.fleet.CreateGroup(ctx, a, toGroupInput(req.Body))
	if err != nil {
		return nil, err
	}
	return CreateGroup201JSONResponse(toGroup(g)), nil
}

func (h *Handlers) GetGroup(ctx context.Context, req GetGroupRequestObject) (GetGroupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	g, err := h.fleet.GetGroup(ctx, a, req.GroupId)
	if err != nil {
		return nil, err
	}
	return GetGroup200JSONResponse(toGroup(g)), nil
}

func (h *Handlers) UpdateGroup(ctx context.Context, req UpdateGroupRequestObject) (UpdateGroupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "name", Message: "is required"}
	}
	g, err := h.fleet.UpdateGroup(ctx, a, req.GroupId, toGroupInput(req.Body))
	if err != nil {
		return nil, err
	}
	return UpdateGroup200JSONResponse(toGroup(g)), nil
}

func (h *Handlers) DeleteGroup(ctx context.Context, req DeleteGroupRequestObject) (DeleteGroupResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.DeleteGroup(ctx, a, req.GroupId); err != nil {
		return nil, err
	}
	return DeleteGroup204Response{}, nil
}

// ---- Runs -------------------------------------------------------------------------

func (h *Handlers) ListRuns(ctx context.Context, req ListRunsRequestObject) (ListRunsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	f := fleet.RunFilter{GroupID: p.GroupId, ServerID: p.ServerId, Status: deref(p.Status), Limit: deref(p.Limit)}
	if p.Kind != nil {
		f.Kind = string(*p.Kind)
	}
	runs, err := h.fleet.ListRuns(ctx, a, f)
	if err != nil {
		return nil, err
	}
	return ListRuns200JSONResponse{Items: mapSlice(runs, toRun)}, nil
}

func (h *Handlers) StartRun(ctx context.Context, req StartRunRequestObject) (StartRunResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, &fleet.ValidationError{Field: "kind", Message: "is required"}
	}
	r := fleet.RunRequest{Kind: fleet.RunKind(b.Kind), GroupID: b.GroupId, Options: b.Options}
	if b.ServerIds != nil {
		r.ServerIDs = *b.ServerIds
	}
	if b.CanaryServerIds != nil {
		r.CanaryServerIDs = *b.CanaryServerIds
	}
	if b.ObserveSeconds != nil {
		r.ObserveSeconds = *b.ObserveSeconds
	}
	r.WaitAfterCanaries = deref(b.WaitAfterCanaries)
	if b.Override != nil {
		r.Override = &fleet.CheckOverride{Checks: b.Override.Checks, Reason: b.Override.Reason}
	}
	run, err := h.fleet.StartRun(ctx, a, r)
	if err != nil {
		return nil, err
	}
	return StartRun202JSONResponse(toRun(run)), nil
}

func (h *Handlers) GetRun(ctx context.Context, req GetRunRequestObject) (GetRunResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	run, err := h.fleet.GetRun(ctx, a, req.RunId)
	if err != nil {
		return nil, err
	}
	return GetRun200JSONResponse(toRun(run)), nil
}

func (h *Handlers) GetPatchReport(ctx context.Context, req GetPatchReportRequestObject) (GetPatchReportResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rep, err := h.fleet.PatchReport(ctx, a, req.RunId)
	if err != nil {
		return nil, err
	}
	return GetPatchReport200JSONResponse(toPatchReport(rep)), nil
}

func toPatchReport(v fleet.PatchReport) PatchReport {
	out := PatchReport{
		RunId: v.RunID, Status: v.Status, Summary: v.Summary,
		ObserveSeconds: v.ObserveSeconds, Canaries: nonNil(v.Canaries),
		Servers: make([]PatchServer, 0, len(v.Servers)),
	}
	for _, s := range v.Servers {
		item := PatchServer{
			ServerId: s.ServerID, Name: s.Name, Status: s.Status, Error: s.Error,
			Canary: s.Canary, Packages: s.Packages, Health: s.Health,
			Installed: nonNil(s.Installed), Held: nonNil(s.Held), NotApproved: nonNil(s.NotApproved),
			Versions: patchVersions(s.Versions),
		}
		item.Checks = make([]struct {
			Blocking   bool    `json:"blocking"`
			Detail     *string `json:"detail,omitempty"`
			Kind       string  `json:"kind"`
			Name       string  `json:"name"`
			Overridden *bool   `json:"overridden,omitempty"`
			Phase      string  `json:"phase"`
			State      string  `json:"state"`
		}, 0, len(s.Checks))
		for _, c := range s.Checks {
			var detail *string
			if c.Detail != "" {
				d := c.Detail
				detail = &d
			}
			var overridden *bool
			if c.Overridden {
				overridden = ptrTo(true)
			}
			item.Checks = append(item.Checks, struct {
				Blocking   bool    `json:"blocking"`
				Detail     *string `json:"detail,omitempty"`
				Kind       string  `json:"kind"`
				Name       string  `json:"name"`
				Overridden *bool   `json:"overridden,omitempty"`
				Phase      string  `json:"phase"`
				State      string  `json:"state"`
			}{Blocking: c.Blocking, Detail: detail, Kind: c.Kind, Name: c.Name, Overridden: overridden, Phase: c.Phase, State: c.State})
		}
		out.Servers = append(out.Servers, item)
	}
	return out
}

// ---- Alerts ------------------------------------------------------------------------

func (h *Handlers) ListIncidents(ctx context.Context, req ListIncidentsRequestObject) (ListIncidentsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.fleet.ListIncidents(ctx, a, fleet.IncidentFilter{All: deref(req.Params.IncludeAcknowledged)})
	if err != nil {
		return nil, err
	}
	return ListIncidents200JSONResponse{Items: mapSlice(items, toIncident)}, nil
}

func (h *Handlers) SnoozeIncident(ctx context.Context, req SnoozeIncidentRequestObject) (SnoozeIncidentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "reason", Message: "is required"}
	}
	v, err := h.fleet.SnoozeIncident(ctx, a, req.IncidentId, req.Body.Until, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return SnoozeIncident200JSONResponse(toIncident(v)), nil
}

func toWindow(v fleet.MaintenanceView) MaintenanceWindow {
	return MaintenanceWindow{
		Id: v.ID, GroupId: v.GroupID, ServerId: v.ServerID, Reason: v.Reason,
		StartsAt: v.StartsAt, EndsAt: v.EndsAt,
	}
}

func (h *Handlers) ListMaintenance(ctx context.Context, _ ListMaintenanceRequestObject) (ListMaintenanceResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.fleet.ListMaintenance(ctx, a)
	if err != nil {
		return nil, err
	}
	return ListMaintenance200JSONResponse{Items: mapSlice(items, toWindow)}, nil
}

func (h *Handlers) CreateMaintenance(ctx context.Context, req CreateMaintenanceRequestObject) (CreateMaintenanceResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "reason", Message: "is required"}
	}
	v, err := h.fleet.CreateMaintenance(ctx, a, req.Body.GroupId, req.Body.ServerId, req.Body.Reason, req.Body.StartsAt, req.Body.EndsAt)
	if err != nil {
		return nil, err
	}
	return CreateMaintenance201JSONResponse(toWindow(v)), nil
}

func (h *Handlers) DeleteMaintenance(ctx context.Context, req DeleteMaintenanceRequestObject) (DeleteMaintenanceResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.DeleteMaintenance(ctx, a, req.WindowId); err != nil {
		return nil, err
	}
	return DeleteMaintenance204Response{}, nil
}

func (h *Handlers) AcknowledgeIncident(ctx context.Context, req AcknowledgeIncidentRequestObject) (AcknowledgeIncidentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	note := ""
	if req.Body != nil {
		note = deref(req.Body.Note)
	}
	v, err := h.fleet.AcknowledgeIncident(ctx, a, req.IncidentId, note)
	if err != nil {
		return nil, err
	}
	return AcknowledgeIncident200JSONResponse(toIncident(v)), nil
}

func (h *Handlers) ListAlerts(ctx context.Context, req ListAlertsRequestObject) (ListAlertsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	al, err := h.fleet.ListAlerts(ctx, a, deref(req.Params.All), deref(req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return ListAlerts200JSONResponse{Items: mapSlice(al, toAlert)}, nil
}

func (h *Handlers) ListAlertRules(ctx context.Context, _ ListAlertRulesRequestObject) (ListAlertRulesResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := h.fleet.ListAlertRules(ctx, a)
	if err != nil {
		return nil, err
	}
	return ListAlertRules200JSONResponse{Items: mapSlice(rules, toAlertRule)}, nil
}

func (h *Handlers) UpdateAlertRule(ctx context.Context, req UpdateAlertRuleRequestObject) (UpdateAlertRuleResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "enabled", Message: "is required"}
	}
	r, err := h.fleet.UpdateAlertRule(ctx, a, req.Kind, req.Body.Enabled, req.Body.Threshold, req.Body.ForMinutes)
	if err != nil {
		return nil, err
	}
	return UpdateAlertRule200JSONResponse(toAlertRule(r)), nil
}

func (h *Handlers) ListChannels(ctx context.Context, _ ListChannelsRequestObject) (ListChannelsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	cs, err := h.fleet.ListChannels(ctx, a)
	if err != nil {
		return nil, err
	}
	return ListChannels200JSONResponse{Items: mapSlice(cs, toChannel)}, nil
}

func (h *Handlers) CreateChannel(ctx context.Context, req CreateChannelRequestObject) (CreateChannelResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, &fleet.ValidationError{Field: "name", Message: "is required"}
	}
	c, err := h.fleet.CreateChannel(ctx, a, b.Name, string(b.Kind), b.Config)
	if err != nil {
		return nil, err
	}
	return CreateChannel201JSONResponse(toChannel(c)), nil
}

func (h *Handlers) UpdateChannel(ctx context.Context, req UpdateChannelRequestObject) (UpdateChannelResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "enabled", Message: "is required"}
	}
	if err := h.fleet.SetChannelEnabled(ctx, a, req.ChannelId, req.Body.Enabled); err != nil {
		return nil, err
	}
	return UpdateChannel204Response{}, nil
}

func (h *Handlers) DeleteChannel(ctx context.Context, req DeleteChannelRequestObject) (DeleteChannelResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.DeleteChannel(ctx, a, req.ChannelId); err != nil {
		return nil, err
	}
	return DeleteChannel204Response{}, nil
}

func (h *Handlers) TestChannel(ctx context.Context, req TestChannelRequestObject) (TestChannelResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.TestChannel(ctx, a, req.ChannelId); err != nil {
		return nil, err
	}
	return TestChannel200JSONResponse{Ok: true}, nil
}

func (h *Handlers) ListCerts(ctx context.Context, _ ListCertsRequestObject) (ListCertsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	cs, err := h.fleet.ListCerts(ctx, a)
	if err != nil {
		return nil, err
	}
	return ListCerts200JSONResponse{Items: mapSlice(cs, toCert)}, nil
}

func (h *Handlers) AddCert(ctx context.Context, req AddCertRequestObject) (AddCertResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "hostname", Message: "is required"}
	}
	c, err := h.fleet.AddCert(ctx, a, req.Body.Hostname, deref(req.Body.Port))
	if err != nil {
		return nil, err
	}
	return AddCert201JSONResponse(toCert(c)), nil
}

func (h *Handlers) DeleteCert(ctx context.Context, req DeleteCertRequestObject) (DeleteCertResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.DeleteCert(ctx, a, req.CertId); err != nil {
		return nil, err
	}
	return DeleteCert204Response{}, nil
}

// ---- Settings --------------------------------------------------------------------------

func (h *Handlers) ListTokens(ctx context.Context, _ ListTokensRequestObject) (ListTokensResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := h.fleet.ListTokens(ctx, a)
	if err != nil {
		return nil, err
	}
	return ListTokens200JSONResponse{Items: mapSlice(ts, toToken)}, nil
}

func (h *Handlers) CreateToken(ctx context.Context, req CreateTokenRequestObject) (CreateTokenResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if b == nil {
		return nil, &fleet.ValidationError{Field: "name", Message: "is required"}
	}
	var scopes []fleet.Scope
	if b.Scopes != nil {
		for _, s := range *b.Scopes {
			scopes = append(scopes, fleet.Scope(s))
		}
	}
	// Absent means the default lifetime; tokens without expiry are not offered.
	var ttl time.Duration
	if b.ExpiresInDays != nil {
		days := *b.ExpiresInDays
		if days < 1 || days > 365 {
			return nil, &fleet.ValidationError{Field: "expiresInDays", Message: "use 1 to 365 days"}
		}
		ttl = time.Duration(days) * 24 * time.Hour
	}
	t, secret, err := h.fleet.CreateToken(ctx, a, b.Name, scopes, ttl)
	if err != nil {
		return nil, err
	}
	return CreateToken201JSONResponse{Token: toToken(t), Secret: secret}, nil
}

func (h *Handlers) RevokeToken(ctx context.Context, req RevokeTokenRequestObject) (RevokeTokenResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.RevokeToken(ctx, a, req.TokenId); err != nil {
		return nil, err
	}
	return RevokeToken204Response{}, nil
}

func (h *Handlers) ListActivity(ctx context.Context, req ListActivityRequestObject) (ListActivityResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	es, err := h.fleet.Activity(ctx, a, deref(req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return ListActivity200JSONResponse{Items: mapSlice(es.Entries, toActivity), Earliest: es.Earliest,
		KeptUntilDeletion: es.KeptUntilDeletion, RetentionHours: es.RetentionHours, HoldUntil: es.HoldUntil}, nil
}

func (h *Handlers) GetSSHKey(ctx context.Context, _ GetSSHKeyRequestObject) (GetSSHKeyResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	pub, fp, err := h.fleet.AccountPublicKey(ctx, a)
	if err != nil {
		return nil, err
	}
	return GetSSHKey200JSONResponse{PublicKey: pub, Fingerprint: fp}, nil
}

func (h *Handlers) GetIncident(ctx context.Context, req GetIncidentRequestObject) (GetIncidentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.fleet.GetIncident(ctx, a, req.IncidentId)
	if err != nil {
		return nil, err
	}
	return GetIncident200JSONResponse(toIncident(v)), nil
}

func (h *Handlers) AcceptIncident(ctx context.Context, req AcceptIncidentRequestObject) (AcceptIncidentResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "reason", Message: "is required"}
	}
	v, err := h.fleet.AcceptIncident(ctx, a, req.IncidentId, req.Body.Until, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return AcceptIncident200JSONResponse(toIncident(v)), nil
}

func (h *Handlers) DecideGate(ctx context.Context, req DecideGateRequestObject) (DecideGateResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "decision", Message: "is required"}
	}
	run, err := h.fleet.DecideGate(ctx, a, req.RunId, string(req.Body.Decision), deref(req.Body.Note))
	if err != nil {
		return nil, err
	}
	return DecideGate200JSONResponse(toRun(run)), nil
}

func patchVersions(vs []proto.PackageVersion) []struct {
	After  string `json:"after"`
	Before string `json:"before"`
	Name   string `json:"name"`
} {
	out := make([]struct {
		After  string `json:"after"`
		Before string `json:"before"`
		Name   string `json:"name"`
	}, 0, len(vs))
	for _, v := range vs {
		out = append(out, struct {
			After  string `json:"after"`
			Before string `json:"before"`
			Name   string `json:"name"`
		}{v.After, v.Before, v.Name})
	}
	return out
}
