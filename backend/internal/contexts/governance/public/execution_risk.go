package public

import (
	"context"
	"errors"
	"time"
)

var ErrUnavailable = errors.New("governance execution risk unavailable")

type ExecutionRiskRequest struct {
	WorkspaceID, SubjectKind, SubjectID           string
	ToolsetVersionID, ToolVersionID, ConnectionID string
	ArgumentsHash, IdempotencyKeyHash             string
	At                                            time.Time
}

type ExecutionRiskDecision struct {
	Sequence, PolicyRevision int64
	PolicyRevisionID         string
	RiskLevel, Outcome       string
	ReasonCodes              []string
}

type ExecutionRisk interface {
	Evaluate(context.Context, ExecutionRiskRequest) (ExecutionRiskDecision, error)
	ConsumeConfirmation(context.Context, ExecutionRiskRequest) (string, error)
}
