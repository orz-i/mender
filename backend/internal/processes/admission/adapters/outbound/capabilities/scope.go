package capabilities

import (
	"context"
	"errors"
	commerce "github.com/orz-i/mender/backend/internal/contexts/commerce/public"
	execution "github.com/orz-i/mender/backend/internal/contexts/execution/public"
	governance "github.com/orz-i/mender/backend/internal/contexts/governance/public"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
)

type Scope struct {
	workspace  string
	commerce   commerce.Reserver
	execution  execution.Admission
	governance governance.ExecutionRisk
}

func riskRequest(v application.RiskRequest) governance.ExecutionRiskRequest {
	return governance.ExecutionRiskRequest{WorkspaceID: v.WorkspaceID, SubjectKind: v.SubjectKind, SubjectID: v.SubjectID, ToolsetVersionID: v.ToolsetVersionID, ToolVersionID: v.ToolVersionID, ConnectionID: v.ConnectionID, ArgumentsHash: v.ArgumentsHash, IdempotencyKeyHash: v.IdempotencyKeyHash, At: v.At}
}
func (s *Scope) EnforceExecutionRisk(ctx context.Context, request application.RiskRequest) error {
	if s == nil || s.governance == nil || request.WorkspaceID != s.workspace {
		return application.ErrForbidden
	}
	value := riskRequest(request)
	decision, err := s.governance.Evaluate(ctx, value)
	if err != nil {
		return application.ErrUnavailable
	}
	switch decision.Outcome {
	case "allow":
		return nil
	case "confirmation_required":
		if request.SubjectKind != "human" {
			return application.ErrForbidden
		}
		id, err := s.governance.ConsumeConfirmation(ctx, value)
		if err != nil {
			return application.ErrUnavailable
		}
		if id == "" {
			return application.ErrForbidden
		}
		return nil
	case "deny":
		return application.ErrForbidden
	default:
		return application.ErrUnavailable
	}
}

func New(workspace string, c commerce.Reserver, e execution.Admission, g governance.ExecutionRisk) *Scope {
	return &Scope{workspace: workspace, commerce: c, execution: e, governance: g}
}
func (s *Scope) FindReplay(ctx context.Context, subject, key string) (application.Record, bool, error) {
	r, found, e := s.execution.FindReplay(ctx, s.workspace, subject, key)
	return application.Record(r), found, e
}
func (s *Scope) Reserve(ctx context.Context, r application.Record) error {
	if r.WorkspaceID != s.workspace {
		return application.ErrForbidden
	}
	err := s.commerce.Reserve(ctx, commerce.ReserveRequest{WorkspaceID: r.WorkspaceID, RunID: r.RunID, ReservationID: r.ReservationID, BudgetID: r.BudgetID, PeriodID: r.PeriodID, Currency: r.Currency, AmountMicro: r.ReservedMicro, At: r.CreatedAt})
	if errors.Is(err, commerce.ErrBudgetExceeded) {
		return application.ErrBudgetExceeded
	}
	if errors.Is(err, commerce.ErrBudgetUnavailable) {
		return application.ErrBudgetUnavailable
	}
	return err
}
func (s *Scope) CreateRun(ctx context.Context, r application.Record) error {
	if r.WorkspaceID != s.workspace {
		return application.ErrForbidden
	}
	return s.execution.CreateRun(ctx, execution.AdmissionRecord(r))
}
func (s *Scope) CreateJob(ctx context.Context, r application.Record) error {
	if r.WorkspaceID != s.workspace {
		return application.ErrForbidden
	}
	return s.execution.CreateJob(ctx, execution.AdmissionRecord(r))
}
func (s *Scope) AppendOutbox(ctx context.Context, r application.Record) error {
	if r.WorkspaceID != s.workspace {
		return application.ErrForbidden
	}
	return s.execution.AppendOutbox(ctx, execution.AdmissionRecord(r))
}

var _ application.Scope = (*Scope)(nil)
