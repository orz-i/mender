package public

import (
	"context"
	"errors"
)

var ErrReleaseRouteBlocked = errors.New("release route blocks new admissions")
var ErrReleaseRouteUnavailable = errors.New("release route unavailable")

type ReleaseRouteQuery struct {
	WorkspaceID               string
	ToolsetVersionID          string
	ToolVersionID             string
	DefaultDeploymentRevision string
}

type ReleaseRouteDecision struct {
	DeploymentRevision string
	ReleasePlanID      string
	ReleaseState       string
	ReleaseRevision    int64
	Routed             bool
}

type ReleaseRouter interface {
	ResolveReleaseRoute(context.Context, ReleaseRouteQuery) (ReleaseRouteDecision, error)
}
