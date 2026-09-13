package application

import (
	"context"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/governance/domain"
)

type PolicyRevision struct {
	WorkspaceID, ID                     string
	Revision                            int64
	State                               string
	MaxRiskLevel                        string
	DenyUnsafeWrite, DenyMCPUnsafeWrite bool
	CreatedByUserID, ActivatedByUserID  string
	CreatedAt, ActivatedAt, RetiredAt   time.Time
}

type PolicyDecision struct {
	Sequence                      int64
	WorkspaceID, PolicyRevisionID string
	PolicyRevision                int64
	TargetKind, TargetID          string
	TargetRevision                int64
	RiskLevel, Outcome            string
	ReasonCodes                   []string
	EvaluatedAt                   time.Time
}

type PolicySnapshot struct {
	Revisions []PolicyRevision
	Decisions []PolicyDecision
}

type PolicyRepository interface {
	ListPolicies(context.Context, string, int) (PolicySnapshot, error)
	CreatePolicy(context.Context, string, string, string, string, bool, bool, time.Time) (PolicyRevision, error)
	ActivatePolicy(context.Context, string, string, string, time.Time) (PolicyRevision, error)
}

type PolicyService struct {
	repository PolicyRepository
	auth       Authorizer
	clock      Clock
}

func NewPolicy(repository PolicyRepository, auth Authorizer, clock Clock) (*PolicyService, error) {
	if repository == nil || auth == nil || clock == nil {
		return nil, ErrUnavailable
	}
	return &PolicyService{repository: repository, auth: auth, clock: clock}, nil
}

func (s *PolicyService) now() (time.Time, error) {
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() {
		return time.Time{}, ErrUnavailable
	}
	return at, nil
}

func policyDomain(v PolicyRevision) domain.PublicationPolicyRevision {
	return domain.PublicationPolicyRevision{
		WorkspaceID: v.WorkspaceID, ID: v.ID, Revision: v.Revision, State: domain.PolicyRevisionState(v.State),
		MaxRiskLevel: domain.PublicationRiskLevel(v.MaxRiskLevel), DenyUnsafeWrite: v.DenyUnsafeWrite,
		DenyMCPUnsafeWrite: v.DenyMCPUnsafeWrite, CreatedByUserID: v.CreatedByUserID, ActivatedByUserID: v.ActivatedByUserID,
		CreatedAt: v.CreatedAt, ActivatedAt: v.ActivatedAt, RetiredAt: v.RetiredAt,
	}
}

func decisionDomain(v PolicyDecision) domain.PublicationPolicyDecision {
	return domain.PublicationPolicyDecision{
		Sequence: v.Sequence, WorkspaceID: v.WorkspaceID, PolicyRevisionID: v.PolicyRevisionID, PolicyRevision: v.PolicyRevision,
		TargetKind: domain.PublicationTarget(v.TargetKind), TargetID: v.TargetID, TargetRevision: v.TargetRevision,
		RiskLevel: domain.PublicationRiskLevel(v.RiskLevel), Outcome: domain.PolicyDecisionOutcome(v.Outcome), ReasonCodes: v.ReasonCodes, EvaluatedAt: v.EvaluatedAt,
	}
}

func validMaxRisk(value string) bool { return domain.PublicationRiskLevel(value).Valid() }

func (s *PolicyService) Snapshot(ctx context.Context, actor Actor, workspace string) (PolicySnapshot, error) {
	if !validID(actor.UserID) || !validID(workspace) {
		return PolicySnapshot{}, ErrForbidden
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:policy"); err != nil {
		return PolicySnapshot{}, err
	}
	snapshot, err := s.repository.ListPolicies(ctx, workspace, 100)
	if err != nil {
		return PolicySnapshot{}, err
	}
	for _, item := range snapshot.Revisions {
		if item.WorkspaceID != workspace || !policyDomain(item).Valid() {
			return PolicySnapshot{}, ErrUnavailable
		}
	}
	for _, item := range snapshot.Decisions {
		if item.WorkspaceID != workspace || !decisionDomain(item).Valid() {
			return PolicySnapshot{}, ErrUnavailable
		}
	}
	return snapshot, nil
}

func (s *PolicyService) Create(ctx context.Context, actor Actor, workspace, id, maxRisk string, denyUnsafe, denyMCPUnsafe bool) (PolicyRevision, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(id) || !validMaxRisk(maxRisk) {
		return PolicyRevision{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:policy"); err != nil {
		return PolicyRevision{}, err
	}
	at, err := s.now()
	if err != nil {
		return PolicyRevision{}, err
	}
	item, err := s.repository.CreatePolicy(ctx, workspace, id, actor.UserID, maxRisk, denyUnsafe, denyMCPUnsafe, at)
	if err != nil {
		return PolicyRevision{}, err
	}
	if item.WorkspaceID != workspace || item.ID != id || !policyDomain(item).Valid() || item.State != string(domain.PolicyDraft) {
		return PolicyRevision{}, ErrUnavailable
	}
	return item, nil
}

func (s *PolicyService) Activate(ctx context.Context, actor Actor, workspace, id string) (PolicyRevision, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(id) {
		return PolicyRevision{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "catalog:policy"); err != nil {
		return PolicyRevision{}, err
	}
	at, err := s.now()
	if err != nil {
		return PolicyRevision{}, err
	}
	item, err := s.repository.ActivatePolicy(ctx, workspace, id, actor.UserID, at)
	if err != nil {
		return PolicyRevision{}, err
	}
	if item.WorkspaceID != workspace || item.ID != id || !policyDomain(item).Valid() || item.State != string(domain.PolicyActive) {
		return PolicyRevision{}, ErrUnavailable
	}
	return item, nil
}
