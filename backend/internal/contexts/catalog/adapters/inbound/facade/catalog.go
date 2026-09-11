package facade

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/catalog/application"
	catalog "github.com/orz-i/mender/backend/internal/contexts/catalog/public"
)

type Catalog struct{ service *application.Service }

func New(service *application.Service) *Catalog { return &Catalog{service: service} }

func (f *Catalog) ResolveToolVersion(ctx context.Context, id, toolID, version string) (catalog.ToolVersion, error) {
	v, err := f.service.Resolve(ctx, id, toolID, version)
	if err != nil {
		switch {
		case errors.Is(err, application.ErrNotFound):
			return catalog.ToolVersion{}, catalog.ErrNotFound
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return catalog.ToolVersion{}, err
		default:
			return catalog.ToolVersion{}, catalog.ErrUnavailable
		}
	}
	return catalog.ToolVersion{ID: v.ID, ToolID: v.ToolID, Version: v.Version, ProviderID: v.ProviderID, PriceVersionID: v.PriceVersionID, DeploymentRevision: v.DeploymentRevision, Title: v.Title, Description: v.Description, InputSchema: v.InputSchema, OutputSchema: v.OutputSchema, SideEffect: v.SideEffect, Idempotency: v.Idempotency, MCPPublishable: v.MCPPublishable}, nil
}

var _ catalog.Catalog = (*Catalog)(nil)
