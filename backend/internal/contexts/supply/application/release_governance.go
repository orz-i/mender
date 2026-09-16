package application

import (
	"context"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
)

type ReleasePlan struct {
	WorkspaceID, ID, PluginID, PluginVersion, ToolsetVersionID, ToolVersionID, ProviderID string
	StableDeploymentRevision, CandidateDeploymentRevision, State, CreatedByUserID         string
	Revision                                                                              int64
	CreatedAt, UpdatedAt, CanaryStartedAt, ObservationUntil, ActivatedAt                  time.Time
	DrainingAt, RolledBackAt, DisabledAt                                                  time.Time
}

type ReleaseRoute struct {
	WorkspaceID, ToolsetVersionID, ToolVersionID, StableDeploymentRevision string
	CandidateDeploymentRevision, Mode, ReleasePlanID                       string
	Revision                                                               int64
	UpdatedAt                                                              time.Time
}

type ReleaseAuditEvent struct {
	Sequence, PlanRevision, RouteRevision            int64
	WorkspaceID, ReleasePlanID, EventKind, RouteMode string
	SelectedDeploymentRevision, ActorUserID, Reason  string
	OccurredAt                                       time.Time
}

type ReleaseSnapshot struct {
	Plans  []ReleasePlan
	Routes []ReleaseRoute
	Events []ReleaseAuditEvent
}

type ReleasePlanInput struct {
	PluginID, PluginVersion, ToolsetVersionID, ToolVersionID, ProviderID string
	StableDeploymentRevision, CandidateDeploymentRevision, Reason        string
}

type ReleaseGovernanceRepository interface {
	ReleaseSnapshot(context.Context, string) (ReleaseSnapshot, error)
	CreateReleasePlan(context.Context, string, string, string, ReleasePlanInput, time.Time) (ReleasePlan, error)
	StartReleaseCanary(context.Context, string, string, string, string, time.Time, time.Time) (ReleasePlan, error)
	PromoteRelease(context.Context, string, string, string, string, time.Time) (ReleasePlan, error)
	DrainRelease(context.Context, string, string, string, string, time.Time) (ReleasePlan, error)
	RollbackRelease(context.Context, string, string, string, string, time.Time) (ReleasePlan, error)
	EmergencyDisableRelease(context.Context, string, string, string, string, time.Time) (ReleasePlan, error)
}

type ReleaseGovernance struct {
	repository ReleaseGovernanceRepository
	authorizer PublisherAuthorizer
	ids        PublicationIDGenerator
	clock      PublicationClock
}

func NewReleaseGovernance(repository ReleaseGovernanceRepository, authorizer PublisherAuthorizer, ids PublicationIDGenerator, clock PublicationClock) (*ReleaseGovernance, error) {
	if repository == nil || authorizer == nil || ids == nil || clock == nil {
		return nil, ErrPublicationUnavailable
	}
	return &ReleaseGovernance{repository: repository, authorizer: authorizer, ids: ids, clock: clock}, nil
}

func releaseNow(clock PublicationClock) (time.Time, error) {
	at := clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() || at.Year() > 9999 {
		return time.Time{}, ErrPublicationUnavailable
	}
	return at, nil
}

func validReleaseReason(reason string) bool {
	runes := []rune(reason)
	return len(runes) >= 1 && len(runes) <= 500
}

func validReleaseInput(input ReleasePlanInput) bool {
	return validPluginID(input.PluginID) && input.PluginVersion != "" && len(input.PluginVersion) <= 128 &&
		validPublisherID(input.ToolsetVersionID) && validPublisherID(input.ToolVersionID) && validPublisherID(input.ProviderID) &&
		validPublisherID(input.StableDeploymentRevision) && validPublisherID(input.CandidateDeploymentRevision) &&
		input.StableDeploymentRevision != input.CandidateDeploymentRevision && validReleaseReason(input.Reason)
}

func releaseDomain(value ReleasePlan) (domain.ReleasePlan, error) {
	return domain.RestoreReleasePlan(domain.ReleasePlanSnapshot{
		WorkspaceID: value.WorkspaceID, ID: value.ID, PluginID: value.PluginID, PluginVersion: value.PluginVersion,
		ToolsetVersionID: value.ToolsetVersionID, ToolVersionID: value.ToolVersionID, ProviderID: value.ProviderID,
		StableDeploymentRevision: value.StableDeploymentRevision, CandidateDeploymentRevision: value.CandidateDeploymentRevision,
		Revision: value.Revision, State: domain.ReleaseState(value.State), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		CanaryStartedAt: value.CanaryStartedAt, ObservationUntil: value.ObservationUntil, ActivatedAt: value.ActivatedAt,
		DrainingAt: value.DrainingAt, RolledBackAt: value.RolledBackAt, DisabledAt: value.DisabledAt,
	})
}

func validateReleaseProjection(value ReleasePlan, workspace, id string) error {
	if value.WorkspaceID != workspace || value.ID != id || !validPublisherID(value.CreatedByUserID) {
		return ErrPublicationUnavailable
	}
	if _, err := releaseDomain(value); err != nil {
		return ErrPublicationUnavailable
	}
	return nil
}

func (s *ReleaseGovernance) Snapshot(ctx context.Context, actor PublisherActor, workspace string) (ReleaseSnapshot, error) {
	if !validPublisherID(actor.UserID) || !validPublisherID(workspace) {
		return ReleaseSnapshot{}, ErrPublicationInvalid
	}
	if err := s.authorizer.Authorize(ctx, actor, workspace, "release:read"); err != nil {
		return ReleaseSnapshot{}, err
	}
	value, err := s.repository.ReleaseSnapshot(ctx, workspace)
	if err != nil {
		return ReleaseSnapshot{}, err
	}
	for _, plan := range value.Plans {
		if err = validateReleaseProjection(plan, workspace, plan.ID); err != nil {
			return ReleaseSnapshot{}, err
		}
	}
	for _, route := range value.Routes {
		if route.WorkspaceID != workspace || !validPublisherID(route.ToolsetVersionID) || !validPublisherID(route.ToolVersionID) || !validPublisherID(route.StableDeploymentRevision) || route.Revision < 1 {
			return ReleaseSnapshot{}, ErrPublicationUnavailable
		}
	}
	return value, nil
}

func (s *ReleaseGovernance) Create(ctx context.Context, actor PublisherActor, workspace string, input ReleasePlanInput) (ReleasePlan, error) {
	if !validPublisherID(actor.UserID) || !validPublisherID(workspace) || !validReleaseInput(input) {
		return ReleasePlan{}, ErrPublicationInvalid
	}
	if err := s.authorizer.Authorize(ctx, actor, workspace, "release:manage"); err != nil {
		return ReleasePlan{}, err
	}
	id, err := s.ids.NewID()
	if err != nil || !validPublisherID(id) {
		return ReleasePlan{}, ErrPublicationUnavailable
	}
	at, err := releaseNow(s.clock)
	if err != nil {
		return ReleasePlan{}, err
	}
	value, err := s.repository.CreateReleasePlan(ctx, workspace, id, actor.UserID, input, at)
	if err != nil {
		return ReleasePlan{}, err
	}
	if err = validateReleaseProjection(value, workspace, id); err != nil {
		return ReleasePlan{}, err
	}
	return value, nil
}

func (s *ReleaseGovernance) StartCanary(ctx context.Context, actor PublisherActor, workspace, id, reason string, observation time.Duration) (ReleasePlan, error) {
	if observation < domain.MinCanaryObservation || observation > domain.MaxCanaryObservation {
		return ReleasePlan{}, ErrPublicationInvalid
	}
	return s.transition(ctx, actor, workspace, id, reason, "canary", observation)
}

func (s *ReleaseGovernance) Promote(ctx context.Context, actor PublisherActor, workspace, id, reason string) (ReleasePlan, error) {
	return s.transition(ctx, actor, workspace, id, reason, "promote", 0)
}
func (s *ReleaseGovernance) Drain(ctx context.Context, actor PublisherActor, workspace, id, reason string) (ReleasePlan, error) {
	return s.transition(ctx, actor, workspace, id, reason, "drain", 0)
}
func (s *ReleaseGovernance) Rollback(ctx context.Context, actor PublisherActor, workspace, id, reason string) (ReleasePlan, error) {
	return s.transition(ctx, actor, workspace, id, reason, "rollback", 0)
}
func (s *ReleaseGovernance) EmergencyDisable(ctx context.Context, actor PublisherActor, workspace, id, reason string) (ReleasePlan, error) {
	return s.transition(ctx, actor, workspace, id, reason, "disable", 0)
}

func (s *ReleaseGovernance) transition(ctx context.Context, actor PublisherActor, workspace, id, reason, action string, observation time.Duration) (ReleasePlan, error) {
	if !validPublisherID(actor.UserID) || !validPublisherID(workspace) || !validPublisherID(id) || !validReleaseReason(reason) {
		return ReleasePlan{}, ErrPublicationInvalid
	}
	if err := s.authorizer.Authorize(ctx, actor, workspace, "release:manage"); err != nil {
		return ReleasePlan{}, err
	}
	at, err := releaseNow(s.clock)
	if err != nil {
		return ReleasePlan{}, err
	}
	var value ReleasePlan
	switch action {
	case "canary":
		value, err = s.repository.StartReleaseCanary(ctx, workspace, id, actor.UserID, reason, at, at.Add(observation))
	case "promote":
		value, err = s.repository.PromoteRelease(ctx, workspace, id, actor.UserID, reason, at)
	case "drain":
		value, err = s.repository.DrainRelease(ctx, workspace, id, actor.UserID, reason, at)
	case "rollback":
		value, err = s.repository.RollbackRelease(ctx, workspace, id, actor.UserID, reason, at)
	case "disable":
		value, err = s.repository.EmergencyDisableRelease(ctx, workspace, id, actor.UserID, reason, at)
	default:
		return ReleasePlan{}, ErrPublicationInvalid
	}
	if err != nil {
		return ReleasePlan{}, err
	}
	if err = validateReleaseProjection(value, workspace, id); err != nil {
		return ReleasePlan{}, err
	}
	return value, nil
}
