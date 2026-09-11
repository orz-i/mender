package facade

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/distribution/application"
	distribution "github.com/orz-i/mender/backend/internal/contexts/distribution/public"
)

type Toolsets struct{ service *application.Service }

func New(service *application.Service) *Toolsets { return &Toolsets{service: service} }

func (f *Toolsets) ResolveBinding(ctx context.Context, workspace, toolsetVersionID, toolID, version string) (distribution.Binding, error) {
	b, err := f.service.Resolve(ctx, workspace, toolsetVersionID, toolID, version)
	if err != nil {
		switch {
		case errors.Is(err, application.ErrNotFound):
			return distribution.Binding{}, distribution.ErrNotFound
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return distribution.Binding{}, err
		default:
			return distribution.Binding{}, distribution.ErrUnavailable
		}
	}
	return distribution.Binding{ToolsetVersionID: b.ToolsetVersionID, ToolID: b.ToolID, ToolVersion: b.ToolVersionLabel, ToolVersionID: b.ToolVersionID, BudgetID: b.BudgetID, ConnectionID: b.ConnectionID, MCPName: b.MCPName}, nil
}

func (f *Toolsets) ListDirectBindings(ctx context.Context, workspace, toolsetVersionID string) ([]distribution.Binding, error) {
	items, err := f.service.ListDirect(ctx, workspace, toolsetVersionID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, distribution.ErrUnavailable
	}
	result := make([]distribution.Binding, 0, len(items))
	for _, item := range items {
		result = append(result, distribution.Binding{ToolsetVersionID: item.ToolsetVersionID, ToolID: item.ToolID, ToolVersion: item.ToolVersionLabel, ToolVersionID: item.ToolVersionID, BudgetID: item.BudgetID, ConnectionID: item.ConnectionID, MCPName: item.MCPName})
	}
	return result, nil
}

var _ distribution.Toolsets = (*Toolsets)(nil)
