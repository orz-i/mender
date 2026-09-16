package application

import (
	"context"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/governance/domain"
)

type PlatformWorkspace struct {
	WorkspaceID, Reason, ActorUserID string
	Frozen                           bool
	Revision                         int64
	UpdatedAt, CreatedAt             time.Time
}

type PlatformProvider struct {
	ProviderID, State, Reason, ActorUserID string
	Revision                               int64
	DeploymentCount, ActiveDeploymentCount int64
	UpdatedAt                              time.Time
}

type PlatformIncident struct {
	ID, TargetKind, TargetID, Severity, Code, State string
	OpenedByUserID, OpenReason                      string
	ResolvedByUserID, Resolution                    string
	Revision                                        int64
	OpenedAt, UpdatedAt, ResolvedAt                 time.Time
}

type PlatformAdminAuditEvent struct {
	Sequence, TargetRevision                     int64
	EventKind, TargetKind, TargetID, ActorUserID string
	Reason                                       string
	OccurredAt                                   time.Time
}

type PlatformIncidentFilter struct {
	State, TargetKind, TargetID, BeforeID string
	BeforeUpdatedAt                       time.Time
	Limit                                 int
}

type PlatformAuditFilter struct {
	BeforeSequence int64
	Limit          int
}

type PlatformAdminRepository interface {
	ListPlatformWorkspaces(context.Context, string) ([]PlatformWorkspace, error)
	SetPlatformWorkspaceFrozen(context.Context, string, int64, bool, string, string, time.Time) (PlatformWorkspace, error)
	ListPlatformProviders(context.Context, string) ([]PlatformProvider, error)
	SetPlatformProviderState(context.Context, string, int64, string, string, string, time.Time) (PlatformProvider, error)
	OpenPlatformIncident(context.Context, string, string, string, string, string, string, string, time.Time) (PlatformIncident, error)
	ResolvePlatformIncident(context.Context, string, int64, string, string, time.Time) (PlatformIncident, error)
	ListPlatformIncidents(context.Context, string, PlatformIncidentFilter) ([]PlatformIncident, error)
	ListPlatformAudit(context.Context, string, PlatformAuditFilter) ([]PlatformAdminAuditEvent, error)
}

type PlatformAdminIDs interface{ NewPlatformIncidentID() (string, error) }

type PlatformAdminService struct {
	repository PlatformAdminRepository
	auth       PlatformAuthorizer
	ids        PlatformAdminIDs
	clock      Clock
}

func NewPlatformAdminService(repository PlatformAdminRepository, auth PlatformAuthorizer, ids PlatformAdminIDs, clock Clock) (*PlatformAdminService, error) {
	if repository == nil || auth == nil || ids == nil || clock == nil {
		return nil, ErrUnavailable
	}
	return &PlatformAdminService{repository: repository, auth: auth, ids: ids, clock: clock}, nil
}

func platformNow(clock Clock) (time.Time, error) {
	at := clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() || at.Year() > 9999 {
		return time.Time{}, ErrUnavailable
	}
	return at, nil
}

func validPlatformReason(value string) bool {
	runes := []rune(value)
	return len(runes) >= 1 && len(runes) <= 1000
}

func validWorkspaceProjection(value PlatformWorkspace) bool {
	return (domain.WorkspaceAdminState{WorkspaceID: value.WorkspaceID, Frozen: value.Frozen, Revision: value.Revision, Reason: value.Reason, ActorUserID: value.ActorUserID, UpdatedAt: value.UpdatedAt}).Valid() && !value.CreatedAt.IsZero() && !value.UpdatedAt.Before(value.CreatedAt)
}

func validProviderProjection(value PlatformProvider) bool {
	return (domain.ProviderAdminState{ProviderID: value.ProviderID, State: value.State, Revision: value.Revision, Reason: value.Reason, ActorUserID: value.ActorUserID, UpdatedAt: value.UpdatedAt}).Valid() && value.DeploymentCount >= 0 && value.ActiveDeploymentCount >= 0 && value.ActiveDeploymentCount <= value.DeploymentCount
}

func incidentDomain(value PlatformIncident) domain.PlatformIncident {
	return domain.PlatformIncident{
		ID: value.ID, TargetKind: value.TargetKind, TargetID: value.TargetID, Severity: value.Severity, Code: value.Code, State: value.State,
		OpenedByUserID: value.OpenedByUserID, OpenReason: value.OpenReason, ResolvedByUserID: value.ResolvedByUserID, Resolution: value.Resolution,
		Revision: value.Revision, OpenedAt: value.OpenedAt, UpdatedAt: value.UpdatedAt, ResolvedAt: value.ResolvedAt,
	}
}

func validPlatformAudit(value PlatformAdminAuditEvent) bool {
	return (domain.PlatformAdminAuditEvent{Sequence: value.Sequence, TargetRevision: value.TargetRevision, EventKind: value.EventKind, TargetKind: value.TargetKind, TargetID: value.TargetID, ActorUserID: value.ActorUserID, Reason: value.Reason, OccurredAt: value.OccurredAt}).Valid()
}

func (s *PlatformAdminService) Workspaces(ctx context.Context, actor Actor) ([]PlatformWorkspace, error) {
	if !validID(actor.UserID) {
		return nil, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "platform:audit"); err != nil {
		return nil, err
	}
	items, err := s.repository.ListPlatformWorkspaces(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if !validWorkspaceProjection(item) {
			return nil, ErrUnavailable
		}
	}
	return items, nil
}

func (s *PlatformAdminService) SetWorkspaceFrozen(ctx context.Context, actor Actor, workspace string, expectedRevision int64, frozen bool, reason string) (PlatformWorkspace, error) {
	if !validID(actor.UserID) || !validID(workspace) || expectedRevision < 1 || !validPlatformReason(reason) {
		return PlatformWorkspace{}, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "platform:operate"); err != nil {
		return PlatformWorkspace{}, err
	}
	at, err := platformNow(s.clock)
	if err != nil {
		return PlatformWorkspace{}, err
	}
	item, err := s.repository.SetPlatformWorkspaceFrozen(ctx, workspace, expectedRevision, frozen, actor.UserID, reason, at)
	if err != nil {
		return PlatformWorkspace{}, err
	}
	if item.WorkspaceID != workspace || item.Frozen != frozen || item.Revision != expectedRevision+1 || item.ActorUserID != actor.UserID || item.Reason != reason || !item.UpdatedAt.Equal(at) || !validWorkspaceProjection(item) {
		return PlatformWorkspace{}, ErrUnavailable
	}
	return item, nil
}

func (s *PlatformAdminService) Providers(ctx context.Context, actor Actor) ([]PlatformProvider, error) {
	if !validID(actor.UserID) {
		return nil, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "platform:review"); err != nil {
		return nil, err
	}
	items, err := s.repository.ListPlatformProviders(ctx, actor.UserID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if !validProviderProjection(item) {
			return nil, ErrUnavailable
		}
	}
	return items, nil
}

func (s *PlatformAdminService) SetProviderState(ctx context.Context, actor Actor, provider string, expectedRevision int64, state, reason string) (PlatformProvider, error) {
	if !validID(actor.UserID) || !validID(provider) || expectedRevision < 1 || (state != domain.ProviderStateActive && state != domain.ProviderStateQuarantined) || !validPlatformReason(reason) {
		return PlatformProvider{}, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "platform:operate"); err != nil {
		return PlatformProvider{}, err
	}
	at, err := platformNow(s.clock)
	if err != nil {
		return PlatformProvider{}, err
	}
	item, err := s.repository.SetPlatformProviderState(ctx, provider, expectedRevision, state, actor.UserID, reason, at)
	if err != nil {
		return PlatformProvider{}, err
	}
	if item.ProviderID != provider || item.State != state || item.Revision != expectedRevision+1 || item.ActorUserID != actor.UserID || item.Reason != reason || !item.UpdatedAt.Equal(at) || !validProviderProjection(item) {
		return PlatformProvider{}, ErrUnavailable
	}
	return item, nil
}

func (s *PlatformAdminService) OpenIncident(ctx context.Context, actor Actor, targetKind, targetID, severity, code, reason string) (PlatformIncident, error) {
	if !validID(actor.UserID) || !validID(targetID) || !validPlatformReason(reason) || (targetKind != domain.PlatformTargetWorkspace && targetKind != domain.PlatformTargetProvider) || (severity != domain.IncidentSeverityInfo && severity != domain.IncidentSeverityWarning && severity != domain.IncidentSeverityCritical) {
		return PlatformIncident{}, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "platform:operate"); err != nil {
		return PlatformIncident{}, err
	}
	id, err := s.ids.NewPlatformIncidentID()
	if err != nil || !validID(id) {
		return PlatformIncident{}, ErrUnavailable
	}
	at, err := platformNow(s.clock)
	if err != nil {
		return PlatformIncident{}, err
	}
	item, err := s.repository.OpenPlatformIncident(ctx, id, targetKind, targetID, severity, code, actor.UserID, reason, at)
	if err != nil {
		return PlatformIncident{}, err
	}
	if item.ID != id || item.TargetKind != targetKind || item.TargetID != targetID || item.Severity != severity || item.Code != code || item.OpenedByUserID != actor.UserID || item.OpenReason != reason || item.State != domain.IncidentStateOpen || item.Revision != 1 || !item.OpenedAt.Equal(at) || !incidentDomain(item).Valid() {
		return PlatformIncident{}, ErrUnavailable
	}
	return item, nil
}

func (s *PlatformAdminService) ResolveIncident(ctx context.Context, actor Actor, id string, expectedRevision int64, resolution string) (PlatformIncident, error) {
	if !validID(actor.UserID) || !validID(id) || expectedRevision < 1 || !validPlatformReason(resolution) {
		return PlatformIncident{}, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "platform:operate"); err != nil {
		return PlatformIncident{}, err
	}
	at, err := platformNow(s.clock)
	if err != nil {
		return PlatformIncident{}, err
	}
	item, err := s.repository.ResolvePlatformIncident(ctx, id, expectedRevision, actor.UserID, resolution, at)
	if err != nil {
		return PlatformIncident{}, err
	}
	if item.ID != id || item.State != domain.IncidentStateResolved || item.Revision != expectedRevision+1 || item.ResolvedByUserID != actor.UserID || item.Resolution != resolution || !item.ResolvedAt.Equal(at) || !incidentDomain(item).Valid() {
		return PlatformIncident{}, ErrUnavailable
	}
	return item, nil
}

func validPlatformIncidentFilter(filter PlatformIncidentFilter) bool {
	if filter.Limit < 1 || filter.Limit > 100 || (filter.State != "" && filter.State != domain.IncidentStateOpen && filter.State != domain.IncidentStateResolved) || (filter.TargetKind != "" && filter.TargetKind != domain.PlatformTargetWorkspace && filter.TargetKind != domain.PlatformTargetProvider) || (filter.TargetID != "" && !validID(filter.TargetID)) {
		return false
	}
	return filter.BeforeUpdatedAt.IsZero() == (filter.BeforeID == "") && (filter.BeforeID == "" || validID(filter.BeforeID))
}

func (s *PlatformAdminService) Incidents(ctx context.Context, actor Actor, filter PlatformIncidentFilter) ([]PlatformIncident, error) {
	if !validID(actor.UserID) || !validPlatformIncidentFilter(filter) {
		return nil, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "platform:audit"); err != nil {
		return nil, err
	}
	items, err := s.repository.ListPlatformIncidents(ctx, actor.UserID, filter)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if !incidentDomain(item).Valid() {
			return nil, ErrUnavailable
		}
	}
	return items, nil
}

func (s *PlatformAdminService) Audit(ctx context.Context, actor Actor, filter PlatformAuditFilter) ([]PlatformAdminAuditEvent, error) {
	if !validID(actor.UserID) || filter.BeforeSequence < 0 || filter.Limit < 1 || filter.Limit > 500 {
		return nil, ErrInvalid
	}
	if err := s.auth.AuthorizePlatform(ctx, actor, "platform:audit"); err != nil {
		return nil, err
	}
	items, err := s.repository.ListPlatformAudit(ctx, actor.UserID, filter)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if !validPlatformAudit(item) {
			return nil, ErrUnavailable
		}
	}
	return items, nil
}
