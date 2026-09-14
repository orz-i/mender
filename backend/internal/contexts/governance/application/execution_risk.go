package application

import (
	"context"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/governance/domain"
)

type ExecutionRiskRequest struct {
	WorkspaceID, SubjectKind, SubjectID           string
	ToolsetVersionID, ToolVersionID, ConnectionID string
	ArgumentsHash, IdempotencyKeyHash             string
	At                                            time.Time
}

type ExecutionRiskDecision struct {
	WorkspaceID, PolicyRevisionID, SubjectKind, SubjectID string
	ToolsetVersionID, ToolVersionID, ConnectionID         string
	ArgumentsHash, IdempotencyKeyHash                     string
	RiskLevel, Outcome                                    string
	ReasonCodes                                           []string
	Sequence, PolicyRevision                              int64
	EvaluatedAt                                           time.Time
}

type ExecutionConfirmationRequest struct {
	ExecutionRiskRequest
	ConfirmationID string
	ExpiresAt      time.Time
}

type ExecutionRiskRepository interface {
	EvaluateExecutionRisk(context.Context, ExecutionRiskRequest) (ExecutionRiskDecision, error)
	CreateExecutionConfirmation(context.Context, ExecutionConfirmationRequest) (ExecutionRiskDecision, string, time.Time, error)
	ConsumeExecutionConfirmation(context.Context, ExecutionRiskRequest) (string, error)
}

type ExecutionRiskCore struct{ repository ExecutionRiskRepository }

func NewExecutionRiskCore(repository ExecutionRiskRepository) (*ExecutionRiskCore, error) {
	if repository == nil {
		return nil, ErrUnavailable
	}
	return &ExecutionRiskCore{repository: repository}, nil
}

func executionHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func validExecutionRiskRequest(v ExecutionRiskRequest) bool {
	return validID(v.WorkspaceID) && (v.SubjectKind == "human" || v.SubjectKind == "machine") && validID(v.SubjectID) &&
		validID(v.ToolsetVersionID) && validID(v.ToolVersionID) && validID(v.ConnectionID) && executionHash(v.ArgumentsHash) &&
		executionHash(v.IdempotencyKeyHash) && !v.At.IsZero()
}

func asExecutionDomain(v ExecutionRiskDecision) domain.ExecutionPolicyDecision {
	return domain.ExecutionPolicyDecision{
		WorkspaceID: v.WorkspaceID, PolicyRevisionID: v.PolicyRevisionID, PolicyRevision: v.PolicyRevision,
		SubjectKind: v.SubjectKind, SubjectID: v.SubjectID, ToolsetVersionID: v.ToolsetVersionID,
		ToolVersionID: v.ToolVersionID, ConnectionID: v.ConnectionID, ArgumentsHash: v.ArgumentsHash,
		IdempotencyKeyHash: v.IdempotencyKeyHash, RiskLevel: v.RiskLevel, Outcome: domain.ExecutionOutcome(v.Outcome),
		ReasonCodes: v.ReasonCodes, Sequence: v.Sequence, EvaluatedAt: v.EvaluatedAt,
	}
}

func (s *ExecutionRiskCore) Evaluate(ctx context.Context, request ExecutionRiskRequest) (ExecutionRiskDecision, error) {
	if !validExecutionRiskRequest(request) {
		return ExecutionRiskDecision{}, ErrInvalid
	}
	item, err := s.repository.EvaluateExecutionRisk(ctx, request)
	if err != nil {
		return ExecutionRiskDecision{}, err
	}
	if item.WorkspaceID != request.WorkspaceID || item.SubjectKind != request.SubjectKind || item.SubjectID != request.SubjectID ||
		item.ToolsetVersionID != request.ToolsetVersionID || item.ToolVersionID != request.ToolVersionID || item.ConnectionID != request.ConnectionID ||
		item.ArgumentsHash != request.ArgumentsHash || item.IdempotencyKeyHash != request.IdempotencyKeyHash || !asExecutionDomain(item).Valid() {
		return ExecutionRiskDecision{}, ErrUnavailable
	}
	return item, nil
}

func (s *ExecutionRiskCore) Confirm(ctx context.Context, request ExecutionConfirmationRequest) (ExecutionRiskDecision, string, time.Time, error) {
	if !validExecutionRiskRequest(request.ExecutionRiskRequest) || !validID(request.ConfirmationID) || !request.ExpiresAt.After(request.At) || request.ExpiresAt.After(request.At.Add(10*time.Minute)) {
		return ExecutionRiskDecision{}, "", time.Time{}, ErrInvalid
	}
	decision, confirmationID, expiresAt, err := s.repository.CreateExecutionConfirmation(ctx, request)
	if err != nil {
		return ExecutionRiskDecision{}, "", time.Time{}, err
	}
	if decision.WorkspaceID != request.WorkspaceID || decision.SubjectKind != "human" || decision.SubjectID != request.SubjectID ||
		decision.ToolsetVersionID != request.ToolsetVersionID || decision.ToolVersionID != request.ToolVersionID || decision.ConnectionID != request.ConnectionID ||
		decision.ArgumentsHash != request.ArgumentsHash || decision.IdempotencyKeyHash != request.IdempotencyKeyHash || !asExecutionDomain(decision).Valid() {
		return ExecutionRiskDecision{}, "", time.Time{}, ErrUnavailable
	}
	if decision.Outcome == string(domain.ExecutionConfirmationRequired) {
		if confirmationID != request.ConfirmationID || !expiresAt.After(request.At) || expiresAt.After(request.ExpiresAt) || expiresAt.After(request.At.Add(10*time.Minute)) {
			return ExecutionRiskDecision{}, "", time.Time{}, ErrUnavailable
		}
	} else if confirmationID != "" || !expiresAt.IsZero() {
		return ExecutionRiskDecision{}, "", time.Time{}, ErrUnavailable
	}
	return decision, confirmationID, expiresAt, nil
}

func (s *ExecutionRiskCore) Consume(ctx context.Context, request ExecutionRiskRequest) (string, error) {
	if !validExecutionRiskRequest(request) || request.SubjectKind != "human" {
		return "", ErrInvalid
	}
	return s.repository.ConsumeExecutionConfirmation(ctx, request)
}

type ConfirmationIDs interface{ NewConfirmationID() (string, error) }

type ExecutionRiskHumanService struct {
	core  *ExecutionRiskCore
	auth  Authorizer
	clock Clock
	ids   ConfirmationIDs
}

func NewExecutionRiskHuman(core *ExecutionRiskCore, auth Authorizer, clock Clock, ids ConfirmationIDs) (*ExecutionRiskHumanService, error) {
	if core == nil || auth == nil || clock == nil || ids == nil {
		return nil, ErrUnavailable
	}
	return &ExecutionRiskHumanService{core: core, auth: auth, clock: clock, ids: ids}, nil
}

type ExecutionRiskTarget struct {
	ToolsetVersionID, ToolVersionID, ConnectionID string
	ArgumentsHash, IdempotencyKeyHash             string
}

func (s *ExecutionRiskHumanService) request(ctx context.Context, actor Actor, workspace string, target ExecutionRiskTarget) (ExecutionRiskRequest, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validID(target.ToolsetVersionID) || !validID(target.ToolVersionID) || !validID(target.ConnectionID) || !executionHash(target.ArgumentsHash) || !executionHash(target.IdempotencyKeyHash) {
		return ExecutionRiskRequest{}, ErrInvalid
	}
	if err := s.auth.Authorize(ctx, actor, workspace, "run:create"); err != nil {
		return ExecutionRiskRequest{}, err
	}
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() {
		return ExecutionRiskRequest{}, ErrUnavailable
	}
	return ExecutionRiskRequest{WorkspaceID: workspace, SubjectKind: "human", SubjectID: actor.UserID, ToolsetVersionID: target.ToolsetVersionID, ToolVersionID: target.ToolVersionID, ConnectionID: target.ConnectionID, ArgumentsHash: target.ArgumentsHash, IdempotencyKeyHash: target.IdempotencyKeyHash, At: at}, nil
}

func (s *ExecutionRiskHumanService) Preview(ctx context.Context, actor Actor, workspace string, target ExecutionRiskTarget) (ExecutionRiskDecision, error) {
	request, err := s.request(ctx, actor, workspace, target)
	if err != nil {
		return ExecutionRiskDecision{}, err
	}
	return s.core.Evaluate(ctx, request)
}

type HumanExecutionConfirmation struct {
	Decision       ExecutionRiskDecision
	ConfirmationID string
	ExpiresAt      time.Time
}

func (s *ExecutionRiskHumanService) Confirm(ctx context.Context, actor Actor, workspace string, target ExecutionRiskTarget) (HumanExecutionConfirmation, error) {
	request, err := s.request(ctx, actor, workspace, target)
	if err != nil {
		return HumanExecutionConfirmation{}, err
	}
	id, err := s.ids.NewConfirmationID()
	if err != nil || !validID(id) {
		return HumanExecutionConfirmation{}, ErrUnavailable
	}
	requestedExpiry := request.At.Add(10 * time.Minute)
	decision, confirmationID, expiresAt, err := s.core.Confirm(ctx, ExecutionConfirmationRequest{ExecutionRiskRequest: request, ConfirmationID: id, ExpiresAt: requestedExpiry})
	if err != nil {
		return HumanExecutionConfirmation{}, err
	}
	return HumanExecutionConfirmation{Decision: decision, ConfirmationID: confirmationID, ExpiresAt: expiresAt}, nil
}
