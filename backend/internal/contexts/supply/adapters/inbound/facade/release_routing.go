package facade

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type ReleaseRouter struct{ service *application.ReleaseRouting }

func NewReleaseRouter(service *application.ReleaseRouting) *ReleaseRouter {
	return &ReleaseRouter{service: service}
}

func (r *ReleaseRouter) ResolveReleaseRoute(ctx context.Context, query supply.ReleaseRouteQuery) (supply.ReleaseRouteDecision, error) {
	if r == nil || r.service == nil {
		return supply.ReleaseRouteDecision{}, supply.ErrReleaseRouteUnavailable
	}
	value, err := r.service.Resolve(ctx, application.ReleaseRoutingQuery{
		WorkspaceID: query.WorkspaceID, ToolsetVersionID: query.ToolsetVersionID, ToolVersionID: query.ToolVersionID, DefaultDeploymentRevision: query.DefaultDeploymentRevision,
	})
	if err != nil {
		switch {
		case errors.Is(err, application.ErrReleaseRoutingBlocked):
			return supply.ReleaseRouteDecision{}, supply.ErrReleaseRouteBlocked
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return supply.ReleaseRouteDecision{}, err
		default:
			return supply.ReleaseRouteDecision{}, supply.ErrReleaseRouteUnavailable
		}
	}
	return supply.ReleaseRouteDecision{DeploymentRevision: value.DeploymentRevision, ReleasePlanID: value.ReleasePlanID, ReleaseState: value.ReleaseState, ReleaseRevision: value.ReleaseRevision, Routed: value.Routed}, nil
}

var _ supply.ReleaseRouter = (*ReleaseRouter)(nil)
