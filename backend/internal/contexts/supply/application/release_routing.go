package application

import (
	"context"
	"errors"
)

var ErrReleaseRoutingBlocked = errors.New("release routing blocks new admission")
var ErrReleaseRoutingUnavailable = errors.New("release routing unavailable")

type ReleaseRoutingQuery struct {
	WorkspaceID               string
	ToolsetVersionID          string
	ToolVersionID             string
	DefaultDeploymentRevision string
}

type ReleaseRoutingDecision struct {
	DeploymentRevision string
	ReleasePlanID      string
	ReleaseState       string
	ReleaseRevision    int64
	Routed             bool
	Blocked            bool
}

type ReleaseRoutingRepository interface {
	ResolveReleaseRoute(context.Context, ReleaseRoutingQuery) (ReleaseRoutingDecision, error)
}

type ReleaseRouting struct{ repository ReleaseRoutingRepository }

func NewReleaseRouting(repository ReleaseRoutingRepository) (*ReleaseRouting, error) {
	if repository == nil {
		return nil, ErrReleaseRoutingUnavailable
	}
	return &ReleaseRouting{repository: repository}, nil
}

func validRouteID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func (s *ReleaseRouting) Resolve(ctx context.Context, query ReleaseRoutingQuery) (ReleaseRoutingDecision, error) {
	if !validRouteID(query.WorkspaceID) || !validRouteID(query.ToolsetVersionID) || !validRouteID(query.ToolVersionID) || !validRouteID(query.DefaultDeploymentRevision) {
		return ReleaseRoutingDecision{}, ErrReleaseRoutingUnavailable
	}
	decision, err := s.repository.ResolveReleaseRoute(ctx, query)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ReleaseRoutingDecision{}, err
		}
		return ReleaseRoutingDecision{}, ErrReleaseRoutingUnavailable
	}
	if decision.Blocked {
		if !decision.Routed || decision.DeploymentRevision != "" || !validRouteID(decision.ReleasePlanID) || decision.ReleaseRevision < 1 || decision.ReleaseState != "disabled" {
			return ReleaseRoutingDecision{}, ErrReleaseRoutingUnavailable
		}
		return decision, ErrReleaseRoutingBlocked
	}
	if !validRouteID(decision.DeploymentRevision) {
		return ReleaseRoutingDecision{}, ErrReleaseRoutingUnavailable
	}
	if !decision.Routed {
		if decision.DeploymentRevision != query.DefaultDeploymentRevision || decision.ReleasePlanID != "" || decision.ReleaseRevision != 0 || decision.ReleaseState != "stable" {
			return ReleaseRoutingDecision{}, ErrReleaseRoutingUnavailable
		}
		return decision, nil
	}
	if !validRouteID(decision.ReleasePlanID) || decision.ReleaseRevision < 1 || (decision.ReleaseState != "stable" && decision.ReleaseState != "canary" && decision.ReleaseState != "draining") {
		return ReleaseRoutingDecision{}, ErrReleaseRoutingUnavailable
	}
	return decision, nil
}
