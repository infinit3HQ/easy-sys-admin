package api

import (
	"context"
	"encoding/json"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

func (h *Handlers) GetServerSbom(ctx context.Context, req GetServerSbomRequestObject) (GetServerSbomResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	bom, err := h.fleet.ServerSBOM(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	out := GetServerSbom200JSONResponse{}
	if err := reshape(bom, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (h *Handlers) GetServerScan(ctx context.Context, req GetServerScanRequestObject) (GetServerScanResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := h.fleet.LatestScan(ctx, a, req.ServerId)
	if err != nil {
		return nil, err
	}
	rep, err := toScanReport(v)
	if err != nil {
		return nil, err
	}
	return GetServerScan200JSONResponse(rep), nil
}

func (h *Handlers) ImportServerScan(ctx context.Context, req ImportServerScanRequestObject) (ImportServerScanResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "scan", Message: "is required"}
	}
	raw, err := json.Marshal(req.Body)
	if err != nil {
		return nil, &fleet.ValidationError{Field: "scan", Message: "could not be read"}
	}
	v, err := h.fleet.ImportScan(ctx, a, req.ServerId, raw)
	if err != nil {
		return nil, err
	}
	rep, err := toScanReport(v)
	if err != nil {
		return nil, err
	}
	return ImportServerScan200JSONResponse(rep), nil
}

func toScanReport(v fleet.ScanView) (ScanReport, error) {
	rep := ScanReport{Imported: v.Imported, Scanner: v.Scanner, ScannerVersion: v.ScannerVersion, DbBuilt: v.DBBuilt,
		Stale: v.Stale, Coverage: v.Coverage}
	if v.Imported {
		at := v.ImportedAt
		rep.ImportedAt = &at
	}
	// The match fields carry the same JSON names on both sides.
	if err := reshape(v.Matches, &rep.Matches); err != nil {
		return ScanReport{}, err
	}
	return rep, nil
}

// reshape copies a value into a generated type with the same JSON shape.
func reshape(from, to any) error {
	raw, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, to)
}
