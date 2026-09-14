package application

import (
	"context"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/governance/domain"
)

const executionGovernanceMaxPage = 100

type ExecutionGovernanceFilter struct {
	ToolVersionID               string
	SubjectKind                 string
	RiskLevel                   string
	Outcome                     string
	PolicyRevision              int64
	ConfirmationState           string
	BeforeDecisionSequence      int64
	BeforeConfirmationCreatedAt time.Time
	BeforeConfirmationID        string
	Limit                       int
}

func validExecutionRiskLevel(value string) bool {
	return value == "low" || value == "medium" || value == "high" || value == "critical"
}

type ExecutionGovernancePolicyRevision struct {
	WorkspaceID, ID, State, MaxUnconfirmedRiskLevel, MaxMachineRiskLevel string
	Revision                                                             int64
	DenyUnsafeWrite                                                      bool
	ConfirmationTTLSeconds                                               int
	CreatedByUserID, ActivatedByUserID                                   string
	CreatedAt, ActivatedAt, RetiredAt                                    time.Time
}

type ExecutionGovernanceConfirmation struct {
	WorkspaceID, ID, UserID, PolicyRevisionID           string
	ToolsetVersionID, ToolVersionID, ConnectionID       string
	ArgumentsHash, IdempotencyKeyHash, RiskLevel, State string
	EffectiveState                                      string
	PolicyRevision                                      int64
	CreatedAt, ExpiresAt, ConsumedAt, ExpiredAt         time.Time
}

type ExecutionGovernanceSnapshot struct {
	Revisions                       []ExecutionGovernancePolicyRevision
	Decisions                       []ExecutionRiskDecision
	Confirmations                   []ExecutionGovernanceConfirmation
	NextBeforeDecisionSequence      int64
	NextBeforeConfirmationCreatedAt time.Time
	NextBeforeConfirmationID        string
}

type ExecutionGovernanceRepository interface {
	ListExecutionGovernance(context.Context, string, ExecutionGovernanceFilter) (ExecutionGovernanceSnapshot, error)
	CreateExecutionPolicy(context.Context, string, string, string, string, string, bool, int, time.Time) (ExecutionGovernancePolicyRevision, error)
	ActivateExecutionPolicy(context.Context, string, string, string, time.Time) (ExecutionGovernancePolicyRevision, error)
}

type ExecutionGovernanceService struct {
	repository ExecutionGovernanceRepository
	auth       Authorizer
	clock      Clock
}

func NewExecutionGovernance(repository ExecutionGovernanceRepository, auth Authorizer, clock Clock) (*ExecutionGovernanceService, error) {
	if repository == nil || auth == nil || clock == nil {
		return nil, ErrUnavailable
	}
	return &ExecutionGovernanceService{repository: repository, auth: auth, clock: clock}, nil
}

func validExecutionOutcome(value string) bool {
	switch domain.ExecutionOutcome(value) {
	case domain.ExecutionAllow, domain.ExecutionConfirmationRequired, domain.ExecutionDeny:
		return true
	default:
		return false
	}
}

func validConfirmationState(value string) bool {
	return value == "active" || value == "consumed" || value == "expired"
}

func validExecutionGovernanceFilter(filter ExecutionGovernanceFilter) bool {
	if filter.ToolVersionID != "" && !validID(filter.ToolVersionID) {
		return false
	}
	if filter.SubjectKind != "" && filter.SubjectKind != "human" && filter.SubjectKind != "machine" {
		return false
	}
	if filter.RiskLevel != "" && !validExecutionRiskLevel(filter.RiskLevel) {
		return false
	}
	if filter.Outcome != "" && !validExecutionOutcome(filter.Outcome) {
		return false
	}
	if filter.PolicyRevision < 0 || filter.BeforeDecisionSequence < 0 || filter.Limit < 1 || filter.Limit > executionGovernanceMaxPage {
		return false
	}
	if filter.ConfirmationState != "" && !validConfirmationState(filter.ConfirmationState) {
		return false
	}
	if filter.BeforeConfirmationCreatedAt.IsZero() != (filter.BeforeConfirmationID == "") {
		return false
	}
	return filter.BeforeConfirmationID == "" || validID(filter.BeforeConfirmationID)
}

func executionPolicyDomain(v ExecutionGovernancePolicyRevision) domain.ExecutionPolicyRevision {
	return domain.ExecutionPolicyRevision{
		WorkspaceID: v.WorkspaceID, ID: v.ID, State: v.State, MaxUnconfirmedRiskLevel: v.MaxUnconfirmedRiskLevel, MaxMachineRiskLevel: v.MaxMachineRiskLevel,
		Revision: v.Revision, DenyUnsafeWrite: v.DenyUnsafeWrite, ConfirmationTTLSeconds: v.ConfirmationTTLSeconds, CreatedByUserID: v.CreatedByUserID,
		ActivatedByUserID: v.ActivatedByUserID, CreatedAt: v.CreatedAt, ActivatedAt: v.ActivatedAt, RetiredAt: v.RetiredAt,
	}
}

func executionRiskRank(value string) int {
	switch value {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	case "critical":
		return 4
	default:
		return 100
	}
}

func validExecutionPolicySettings(maxUnconfirmed, maxMachine string, ttlSeconds int) bool {
	return validExecutionRiskLevel(maxUnconfirmed) && validExecutionRiskLevel(maxMachine) &&
		executionRiskRank(maxMachine) <= executionRiskRank(maxUnconfirmed) && ttlSeconds >= 30 && ttlSeconds <= 600
}

func (s *ExecutionGovernanceService) now() (time.Time, error) {
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() {
		return time.Time{}, ErrUnavailable
	}
	return at, nil
}

func executionConfirmationDomain(v ExecutionGovernanceConfirmation) domain.ExecutionConfirmation {
	return domain.ExecutionConfirmation{
		WorkspaceID: v.WorkspaceID, ID: v.ID, UserID: v.UserID, PolicyRevisionID: v.PolicyRevisionID,
		PolicyRevision: v.PolicyRevision, ToolsetVersionID: v.ToolsetVersionID, ToolVersionID: v.ToolVersionID,
		ConnectionID: v.ConnectionID, ArgumentsHash: v.ArgumentsHash, IdempotencyKeyHash: v.IdempotencyKeyHash,
		RiskLevel: v.RiskLevel, State: v.State, CreatedAt: v.CreatedAt, ExpiresAt: v.ExpiresAt,
		ConsumedAt: v.ConsumedAt, ExpiredAt: v.ExpiredAt,
	}
}

func (s *ExecutionGovernanceService) Snapshot(ctx context.Context, actor Actor, workspace string, filter ExecutionGovernanceFilter) (ExecutionGovernanceSnapshot, error) {
	if !validID(actor.UserID) || !validID(workspace) {
		return ExecutionGovernanceSnapshot{}, ErrForbidden
	}
	if !validExecutionGovernanceFilter(filter) {
		return ExecutionGovernanceSnapshot{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "execution:governance"); err != nil {
		return ExecutionGovernanceSnapshot{}, err
	}
	snapshot, err := s.repository.ListExecutionGovernance(ctx, workspace, filter)
	if err != nil {
		return ExecutionGovernanceSnapshot{}, err
	}
	if len(snapshot.Revisions) > executionGovernanceMaxPage || len(snapshot.Decisions) > filter.Limit || len(snapshot.Confirmations) > filter.Limit ||
		snapshot.NextBeforeDecisionSequence < 0 || snapshot.NextBeforeConfirmationCreatedAt.IsZero() != (snapshot.NextBeforeConfirmationID == "") {
		return ExecutionGovernanceSnapshot{}, ErrUnavailable
	}
	for _, revision := range snapshot.Revisions {
		if revision.WorkspaceID != workspace || !executionPolicyDomain(revision).Valid() {
			return ExecutionGovernanceSnapshot{}, ErrUnavailable
		}
	}
	var previousDecision int64
	for i, decision := range snapshot.Decisions {
		if decision.WorkspaceID != workspace || !asExecutionDomain(decision).Valid() || (i > 0 && decision.Sequence >= previousDecision) {
			return ExecutionGovernanceSnapshot{}, ErrUnavailable
		}
		if filter.BeforeDecisionSequence > 0 && decision.Sequence >= filter.BeforeDecisionSequence {
			return ExecutionGovernanceSnapshot{}, ErrUnavailable
		}
		if filter.ToolVersionID != "" && decision.ToolVersionID != filter.ToolVersionID ||
			filter.SubjectKind != "" && decision.SubjectKind != filter.SubjectKind ||
			filter.RiskLevel != "" && decision.RiskLevel != filter.RiskLevel ||
			filter.Outcome != "" && decision.Outcome != filter.Outcome ||
			filter.PolicyRevision > 0 && decision.PolicyRevision != filter.PolicyRevision {
			return ExecutionGovernanceSnapshot{}, ErrUnavailable
		}
		previousDecision = decision.Sequence
	}
	if snapshot.NextBeforeDecisionSequence > 0 && (len(snapshot.Decisions) == 0 || snapshot.NextBeforeDecisionSequence != snapshot.Decisions[len(snapshot.Decisions)-1].Sequence) {
		return ExecutionGovernanceSnapshot{}, ErrUnavailable
	}
	var previousConfirmationCreatedAt time.Time
	var previousConfirmationID string
	for i, confirmation := range snapshot.Confirmations {
		if confirmation.WorkspaceID != workspace || !executionConfirmationDomain(confirmation).Valid() {
			return ExecutionGovernanceSnapshot{}, ErrUnavailable
		}
		if !validConfirmationState(confirmation.EffectiveState) ||
			confirmation.State == "consumed" && confirmation.EffectiveState != "consumed" ||
			confirmation.State == "expired" && confirmation.EffectiveState != "expired" ||
			confirmation.State == "active" && confirmation.EffectiveState != "active" && confirmation.EffectiveState != "expired" {
			return ExecutionGovernanceSnapshot{}, ErrUnavailable
		}
		if filter.ToolVersionID != "" && confirmation.ToolVersionID != filter.ToolVersionID ||
			filter.SubjectKind == "machine" ||
			filter.RiskLevel != "" && confirmation.RiskLevel != filter.RiskLevel ||
			filter.PolicyRevision > 0 && confirmation.PolicyRevision != filter.PolicyRevision ||
			filter.ConfirmationState != "" && confirmation.EffectiveState != filter.ConfirmationState {
			return ExecutionGovernanceSnapshot{}, ErrUnavailable
		}
		if !filter.BeforeConfirmationCreatedAt.IsZero() && !(confirmation.CreatedAt.Before(filter.BeforeConfirmationCreatedAt) || confirmation.CreatedAt.Equal(filter.BeforeConfirmationCreatedAt) && confirmation.ID < filter.BeforeConfirmationID) {
			return ExecutionGovernanceSnapshot{}, ErrUnavailable
		}
		if i > 0 && !(confirmation.CreatedAt.Before(previousConfirmationCreatedAt) || confirmation.CreatedAt.Equal(previousConfirmationCreatedAt) && confirmation.ID < previousConfirmationID) {
			return ExecutionGovernanceSnapshot{}, ErrUnavailable
		}
		previousConfirmationCreatedAt, previousConfirmationID = confirmation.CreatedAt, confirmation.ID
	}
	if !snapshot.NextBeforeConfirmationCreatedAt.IsZero() && (len(snapshot.Confirmations) == 0 ||
		!snapshot.NextBeforeConfirmationCreatedAt.Equal(snapshot.Confirmations[len(snapshot.Confirmations)-1].CreatedAt) ||
		snapshot.NextBeforeConfirmationID != snapshot.Confirmations[len(snapshot.Confirmations)-1].ID) {
		return ExecutionGovernanceSnapshot{}, ErrUnavailable
	}
	return snapshot, nil
}

func (s *ExecutionGovernanceService) CreatePolicy(ctx context.Context, actor Actor, workspace, id, maxUnconfirmed, maxMachine string, denyUnsafe bool, ttlSeconds int) (ExecutionGovernancePolicyRevision, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(id) || !validExecutionPolicySettings(maxUnconfirmed, maxMachine, ttlSeconds) {
		return ExecutionGovernancePolicyRevision{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "execution:governance"); err != nil {
		return ExecutionGovernancePolicyRevision{}, err
	}
	at, err := s.now()
	if err != nil {
		return ExecutionGovernancePolicyRevision{}, err
	}
	item, err := s.repository.CreateExecutionPolicy(ctx, workspace, id, actor.UserID, maxUnconfirmed, maxMachine, denyUnsafe, ttlSeconds, at)
	if err != nil {
		return ExecutionGovernancePolicyRevision{}, err
	}
	if item.WorkspaceID != workspace || item.ID != id || item.State != "draft" || !executionPolicyDomain(item).Valid() ||
		item.MaxUnconfirmedRiskLevel != maxUnconfirmed || item.MaxMachineRiskLevel != maxMachine || item.DenyUnsafeWrite != denyUnsafe || item.ConfirmationTTLSeconds != ttlSeconds {
		return ExecutionGovernancePolicyRevision{}, ErrUnavailable
	}
	return item, nil
}

func (s *ExecutionGovernanceService) ActivatePolicy(ctx context.Context, actor Actor, workspace, id string) (ExecutionGovernancePolicyRevision, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(id) {
		return ExecutionGovernancePolicyRevision{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "execution:governance"); err != nil {
		return ExecutionGovernancePolicyRevision{}, err
	}
	at, err := s.now()
	if err != nil {
		return ExecutionGovernancePolicyRevision{}, err
	}
	item, err := s.repository.ActivateExecutionPolicy(ctx, workspace, id, actor.UserID, at)
	if err != nil {
		return ExecutionGovernancePolicyRevision{}, err
	}
	if item.WorkspaceID != workspace || item.ID != id || item.State != "active" || !executionPolicyDomain(item).Valid() {
		return ExecutionGovernancePolicyRevision{}, ErrUnavailable
	}
	return item, nil
}
