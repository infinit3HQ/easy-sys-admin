package api

import (
	"context"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/fleet"
)

func (h *Handlers) ListDatasets(ctx context.Context, _ ListDatasetsRequestObject) (ListDatasetsResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	items, err := h.fleet.ListDatasets(ctx, a)
	if err != nil {
		return nil, err
	}
	out := make([]Dataset, 0, len(items))
	for _, d := range items {
		out = append(out, Dataset{Id: d.ID, Name: d.Name, MaxAgeHours: d.MaxAgeHours})
	}
	return ListDatasets200JSONResponse{Items: out}, nil
}

func (h *Handlers) CreateDataset(ctx context.Context, req CreateDatasetRequestObject) (CreateDatasetResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, &fleet.ValidationError{Field: "name", Message: "is required"}
	}
	d, err := h.fleet.CreateDataset(ctx, a, req.Body.Name, req.Body.MaxAgeHours)
	if err != nil {
		return nil, err
	}
	return CreateDataset201JSONResponse{Id: d.ID, Name: d.Name, MaxAgeHours: d.MaxAgeHours}, nil
}

func (h *Handlers) DeleteDataset(ctx context.Context, req DeleteDatasetRequestObject) (DeleteDatasetResponseObject, error) {
	a, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.fleet.DeleteDataset(ctx, a, req.DatasetId); err != nil {
		return nil, err
	}
	return DeleteDataset204Response{}, nil
}
