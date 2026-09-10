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
	return distribution.Binding{ToolsetVersionID: b.ToolsetVersionID, ToolVersionID: b.ToolVersionID, BudgetID: b.BudgetID}, nil
}

var _ distribution.Toolsets = (*Toolsets)(nil)
