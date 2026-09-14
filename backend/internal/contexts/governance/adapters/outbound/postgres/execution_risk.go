package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type executionRiskQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type ExecutionRiskTxRepository struct{ q executionRiskQuerier }

func NewExecutionRiskTx(q executionRiskQuerier) *ExecutionRiskTxRepository {
	return &ExecutionRiskTxRepository{q: q}
}

func scanExecutionDecision(row pgx.Row) (application.ExecutionRiskDecision, error) {
	var item application.ExecutionRiskDecision
	err := row.Scan(&item.Sequence, &item.WorkspaceID, &item.PolicyRevisionID, &item.PolicyRevision, &item.SubjectKind, &item.SubjectID,
		&item.ToolsetVersionID, &item.ToolVersionID, &item.ConnectionID, &item.ArgumentsHash, &item.IdempotencyKeyHash,
		&item.RiskLevel, &item.Outcome, &item.ReasonCodes, &item.EvaluatedAt)
	return item, mapErr(err)
}

const executionDecisionCols = `sequence,workspace_id,policy_revision_id,policy_revision,subject_kind,subject_id,toolset_version_id,tool_version_id,connection_id,arguments_hash,idempotency_key_hash,risk_level,outcome,reason_codes,evaluated_at`

func (r *ExecutionRiskTxRepository) EvaluateExecutionRisk(ctx context.Context, request application.ExecutionRiskRequest) (application.ExecutionRiskDecision, error) {
	if r == nil || r.q == nil {
		return application.ExecutionRiskDecision{}, application.ErrUnavailable
	}
	var sequence int64
	err := r.q.QueryRow(ctx, `SELECT governance.evaluate_execution_policy($1,$2,$3,$4,$5,$6,$7,$8,$9)`, request.WorkspaceID, request.SubjectKind, request.SubjectID, request.ToolsetVersionID, request.ToolVersionID, request.ConnectionID, request.ArgumentsHash, request.IdempotencyKeyHash, request.At).Scan(&sequence)
	if err != nil {
		return application.ExecutionRiskDecision{}, mapErr(err)
	}
	return scanExecutionDecision(r.q.QueryRow(ctx, `SELECT `+executionDecisionCols+` FROM governance.execution_policy_decisions WHERE sequence=$1`, sequence))
}

func (r *ExecutionRiskTxRepository) CreateExecutionConfirmation(ctx context.Context, request application.ExecutionConfirmationRequest) (application.ExecutionRiskDecision, string, time.Time, error) {
	if r == nil || r.q == nil {
		return application.ExecutionRiskDecision{}, "", time.Time{}, application.ErrUnavailable
	}
	var sequence int64
	var confirmationID *string
	err := r.q.QueryRow(ctx, `SELECT decision_sequence,confirmation_id_result FROM governance.create_execution_confirmation($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, request.WorkspaceID, request.ConfirmationID, request.SubjectID, request.ToolsetVersionID, request.ToolVersionID, request.ConnectionID, request.ArgumentsHash, request.IdempotencyKeyHash, request.At, request.ExpiresAt).Scan(&sequence, &confirmationID)
	if err != nil {
		return application.ExecutionRiskDecision{}, "", time.Time{}, mapErr(err)
	}
	decision, err := scanExecutionDecision(r.q.QueryRow(ctx, `SELECT `+executionDecisionCols+` FROM governance.execution_policy_decisions WHERE sequence=$1`, sequence))
	if err != nil {
		return application.ExecutionRiskDecision{}, "", time.Time{}, err
	}
	if confirmationID == nil {
		return decision, "", time.Time{}, nil
	}
	var expiresAt time.Time
	if err = r.q.QueryRow(ctx, `SELECT expires_at FROM governance.execution_confirmations WHERE workspace_id=$1 AND id=$2`, request.WorkspaceID, *confirmationID).Scan(&expiresAt); err != nil {
		return application.ExecutionRiskDecision{}, "", time.Time{}, mapErr(err)
	}
	return decision, *confirmationID, expiresAt, nil
}

func (r *ExecutionRiskTxRepository) ConsumeExecutionConfirmation(ctx context.Context, request application.ExecutionRiskRequest) (string, error) {
	if r == nil || r.q == nil {
		return "", application.ErrUnavailable
	}
	var id *string
	err := r.q.QueryRow(ctx, `SELECT governance.consume_execution_confirmation($1,$2,$3,$4,$5,$6,$7,$8)`, request.WorkspaceID, request.SubjectID, request.ToolsetVersionID, request.ToolVersionID, request.ConnectionID, request.ArgumentsHash, request.IdempotencyKeyHash, request.At).Scan(&id)
	if err != nil {
		return "", mapErr(err)
	}
	if id == nil {
		return "", nil
	}
	return *id, nil
}

type ExecutionRiskPoolRepository struct{ pool *pgxpool.Pool }

func NewExecutionRiskPool(pool *pgxpool.Pool) *ExecutionRiskPoolRepository {
	return &ExecutionRiskPoolRepository{pool: pool}
}

func (r *ExecutionRiskPoolRepository) within(ctx context.Context, workspace string, fn func(*ExecutionRiskTxRepository) error) error {
	if r == nil || r.pool == nil {
		return application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return application.ErrUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		return application.ErrUnavailable
	}
	if err = fn(NewExecutionRiskTx(tx)); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ErrUnavailable
	}
	return nil
}

func (r *ExecutionRiskPoolRepository) EvaluateExecutionRisk(ctx context.Context, request application.ExecutionRiskRequest) (item application.ExecutionRiskDecision, err error) {
	err = r.within(ctx, request.WorkspaceID, func(tx *ExecutionRiskTxRepository) error {
		item, err = tx.EvaluateExecutionRisk(ctx, request)
		return err
	})
	return item, err
}

func (r *ExecutionRiskPoolRepository) CreateExecutionConfirmation(ctx context.Context, request application.ExecutionConfirmationRequest) (item application.ExecutionRiskDecision, confirmation string, expiresAt time.Time, err error) {
	err = r.within(ctx, request.WorkspaceID, func(tx *ExecutionRiskTxRepository) error {
		item, confirmation, expiresAt, err = tx.CreateExecutionConfirmation(ctx, request)
		return err
	})
	return item, confirmation, expiresAt, err
}

func (r *ExecutionRiskPoolRepository) ConsumeExecutionConfirmation(ctx context.Context, request application.ExecutionRiskRequest) (id string, err error) {
	err = r.within(ctx, request.WorkspaceID, func(tx *ExecutionRiskTxRepository) error {
		id, err = tx.ConsumeExecutionConfirmation(ctx, request)
		return err
	})
	return id, err
}

var _ application.ExecutionRiskRepository = (*ExecutionRiskTxRepository)(nil)
var _ application.ExecutionRiskRepository = (*ExecutionRiskPoolRepository)(nil)
