package admission

import (
	"context"

	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
	governance "github.com/orz-i/mender/backend/internal/contexts/governance/public"
)

type ExecutionRisk struct {
	core *application.ExecutionRiskCore
}

func NewExecutionRisk(core *application.ExecutionRiskCore) *ExecutionRisk {
	return &ExecutionRisk{core: core}
}

func appRequest(v governance.ExecutionRiskRequest) application.ExecutionRiskRequest {
	return application.ExecutionRiskRequest{WorkspaceID: v.WorkspaceID, SubjectKind: v.SubjectKind, SubjectID: v.SubjectID, ToolsetVersionID: v.ToolsetVersionID, ToolVersionID: v.ToolVersionID, ConnectionID: v.ConnectionID, ArgumentsHash: v.ArgumentsHash, IdempotencyKeyHash: v.IdempotencyKeyHash, At: v.At}
}

func (g *ExecutionRisk) Evaluate(ctx context.Context, request governance.ExecutionRiskRequest) (governance.ExecutionRiskDecision, error) {
	if g == nil || g.core == nil {
		return governance.ExecutionRiskDecision{}, governance.ErrUnavailable
	}
	item, err := g.core.Evaluate(ctx, appRequest(request))
	if err != nil {
		return governance.ExecutionRiskDecision{}, governance.ErrUnavailable
	}
	return governance.ExecutionRiskDecision{Sequence: item.Sequence, PolicyRevisionID: item.PolicyRevisionID, PolicyRevision: item.PolicyRevision, RiskLevel: item.RiskLevel, Outcome: item.Outcome, ReasonCodes: append([]string(nil), item.ReasonCodes...)}, nil
}

func (g *ExecutionRisk) ConsumeConfirmation(ctx context.Context, request governance.ExecutionRiskRequest) (string, error) {
	if g == nil || g.core == nil {
		return "", governance.ErrUnavailable
	}
	id, err := g.core.Consume(ctx, appRequest(request))
	if err != nil {
		return "", governance.ErrUnavailable
	}
	return id, nil
}

var _ governance.ExecutionRisk = (*ExecutionRisk)(nil)
