package governanceaccess

import (
	"context"
	"errors"

	governance "github.com/orz-i/mender/backend/internal/contexts/governance/public"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
)

type Evaluator struct{ governance governance.ExecutionRisk }

func New(governanceRisk governance.ExecutionRisk) *Evaluator {
	return &Evaluator{governance: governanceRisk}
}

func publicRequest(v application.RiskRequest) governance.ExecutionRiskRequest {
	return governance.ExecutionRiskRequest{WorkspaceID: v.WorkspaceID, SubjectKind: v.SubjectKind, SubjectID: v.SubjectID, ToolsetVersionID: v.ToolsetVersionID, ToolVersionID: v.ToolVersionID, ConnectionID: v.ConnectionID, ArgumentsHash: v.ArgumentsHash, IdempotencyKeyHash: v.IdempotencyKeyHash, At: v.At}
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return application.ErrUnavailable
}

func (e *Evaluator) Evaluate(ctx context.Context, request application.RiskRequest) (application.RiskDecision, error) {
	if e == nil || e.governance == nil {
		return application.RiskDecision{}, application.ErrUnavailable
	}
	decision, err := e.governance.Evaluate(ctx, publicRequest(request))
	if err != nil {
		return application.RiskDecision{}, mapError(err)
	}
	return application.RiskDecision{Outcome: decision.Outcome}, nil
}

var _ application.RiskEvaluator = (*Evaluator)(nil)
