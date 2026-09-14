package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type ExecutionGovernanceRepository struct{ pool *pgxpool.Pool }

func NewExecutionGovernance(pool *pgxpool.Pool) *ExecutionGovernanceRepository {
	return &ExecutionGovernanceRepository{pool: pool}
}

func (r *ExecutionGovernanceRepository) begin(ctx context.Context, workspace string) (pgx.Tx, error) {
	if r == nil || r.pool == nil {
		return nil, application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		rollback(tx)
		return nil, application.ErrUnavailable
	}
	return tx, nil
}

func scanExecutionGovernancePolicy(row pgx.Row) (application.ExecutionGovernancePolicyRevision, error) {
	var item application.ExecutionGovernancePolicyRevision
	var createdBy, activatedBy *string
	var activatedAt, retiredAt *time.Time
	err := row.Scan(&item.WorkspaceID, &item.ID, &item.Revision, &item.State, &item.MaxUnconfirmedRiskLevel,
		&item.MaxMachineRiskLevel, &item.DenyUnsafeWrite, &item.ConfirmationTTLSeconds, &createdBy, &item.CreatedAt, &activatedBy, &activatedAt, &retiredAt)
	if createdBy != nil {
		item.CreatedByUserID = *createdBy
	}
	if activatedBy != nil {
		item.ActivatedByUserID = *activatedBy
	}
	if activatedAt != nil {
		item.ActivatedAt = *activatedAt
	}
	if retiredAt != nil {
		item.RetiredAt = *retiredAt
	}
	return item, mapErr(err)
}

func scanExecutionGovernanceConfirmation(row pgx.Row) (application.ExecutionGovernanceConfirmation, error) {
	var item application.ExecutionGovernanceConfirmation
	var consumedAt, expiredAt *time.Time
	err := row.Scan(&item.WorkspaceID, &item.ID, &item.UserID, &item.PolicyRevisionID, &item.PolicyRevision,
		&item.ToolsetVersionID, &item.ToolVersionID, &item.ConnectionID, &item.ArgumentsHash, &item.IdempotencyKeyHash,
		&item.RiskLevel, &item.State, &item.EffectiveState, &item.CreatedAt, &item.ExpiresAt, &consumedAt, &expiredAt)
	if consumedAt != nil {
		item.ConsumedAt = *consumedAt
	}
	if expiredAt != nil {
		item.ExpiredAt = *expiredAt
	}
	return item, mapErr(err)
}

const executionGovernancePolicyCols = `workspace_id,id,revision,state,max_unconfirmed_risk_level,max_machine_risk_level,deny_unsafe_write,confirmation_ttl_seconds,created_by_user_id,created_at,activated_by_user_id,activated_at,retired_at`

func (r *ExecutionGovernanceRepository) ListExecutionGovernance(ctx context.Context, workspace string, filter application.ExecutionGovernanceFilter) (application.ExecutionGovernanceSnapshot, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.ExecutionGovernanceSnapshot{}, err
	}
	defer rollback(tx)

	snapshot := application.ExecutionGovernanceSnapshot{Revisions: []application.ExecutionGovernancePolicyRevision{}, Decisions: []application.ExecutionRiskDecision{}, Confirmations: []application.ExecutionGovernanceConfirmation{}}
	rows, err := tx.Query(ctx, `SELECT `+executionGovernancePolicyCols+` FROM governance.execution_policy_revisions WHERE workspace_id=$1 ORDER BY revision DESC LIMIT 100`, workspace)
	if err != nil {
		return application.ExecutionGovernanceSnapshot{}, mapErr(err)
	}
	for rows.Next() {
		item, scanErr := scanExecutionGovernancePolicy(rows)
		if scanErr != nil {
			rows.Close()
			return application.ExecutionGovernanceSnapshot{}, scanErr
		}
		snapshot.Revisions = append(snapshot.Revisions, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return application.ExecutionGovernanceSnapshot{}, application.ErrUnavailable
	}

	rows, err = tx.Query(ctx, `SELECT `+executionDecisionCols+`
		FROM governance.execution_policy_decisions
		WHERE workspace_id=$1
		  AND ($2='' OR tool_version_id=$2)
		  AND ($3='' OR subject_kind=$3)
		  AND ($4='' OR risk_level=$4)
		  AND ($5='' OR outcome=$5)
		  AND ($6::bigint=0 OR policy_revision=$6)
		  AND ($7::bigint=0 OR sequence<$7)
		ORDER BY sequence DESC LIMIT $8`, workspace, filter.ToolVersionID, filter.SubjectKind, filter.RiskLevel, filter.Outcome, filter.PolicyRevision, filter.BeforeDecisionSequence, filter.Limit+1)
	if err != nil {
		return application.ExecutionGovernanceSnapshot{}, mapErr(err)
	}
	for rows.Next() {
		item, scanErr := scanExecutionDecision(rows)
		if scanErr != nil {
			rows.Close()
			return application.ExecutionGovernanceSnapshot{}, scanErr
		}
		snapshot.Decisions = append(snapshot.Decisions, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return application.ExecutionGovernanceSnapshot{}, application.ErrUnavailable
	}
	if len(snapshot.Decisions) > filter.Limit {
		snapshot.Decisions = snapshot.Decisions[:filter.Limit]
		snapshot.NextBeforeDecisionSequence = snapshot.Decisions[len(snapshot.Decisions)-1].Sequence
	}

	var beforeCreated any
	if !filter.BeforeConfirmationCreatedAt.IsZero() {
		beforeCreated = filter.BeforeConfirmationCreatedAt
	}
	rows, err = tx.Query(ctx, `SELECT workspace_id,id,user_id,policy_revision_id,policy_revision,
		toolset_version_id,tool_version_id,connection_id,arguments_hash,idempotency_key_hash,risk_level,state,
		CASE WHEN state='active' AND expires_at<=clock_timestamp() THEN 'expired' ELSE state END AS effective_state,
		created_at,expires_at,consumed_at,expired_at
		FROM governance.execution_confirmations
		WHERE workspace_id=$1
		  AND ($2='' OR tool_version_id=$2)
		  AND ($3='' OR $3='human')
		  AND ($4='' OR risk_level=$4)
		  AND ($5::bigint=0 OR policy_revision=$5)
		  AND ($6='' OR CASE WHEN state='active' AND expires_at<=clock_timestamp() THEN 'expired' ELSE state END=$6)
		  AND ($7::timestamptz IS NULL OR created_at<$7 OR (created_at=$7 AND id<$8))
		ORDER BY created_at DESC,id DESC LIMIT $9`, workspace, filter.ToolVersionID, filter.SubjectKind, filter.RiskLevel, filter.PolicyRevision, filter.ConfirmationState, beforeCreated, filter.BeforeConfirmationID, filter.Limit+1)
	if err != nil {
		return application.ExecutionGovernanceSnapshot{}, mapErr(err)
	}
	for rows.Next() {
		item, scanErr := scanExecutionGovernanceConfirmation(rows)
		if scanErr != nil {
			rows.Close()
			return application.ExecutionGovernanceSnapshot{}, scanErr
		}
		snapshot.Confirmations = append(snapshot.Confirmations, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return application.ExecutionGovernanceSnapshot{}, application.ErrUnavailable
	}
	if len(snapshot.Confirmations) > filter.Limit {
		snapshot.Confirmations = snapshot.Confirmations[:filter.Limit]
		last := snapshot.Confirmations[len(snapshot.Confirmations)-1]
		snapshot.NextBeforeConfirmationCreatedAt = last.CreatedAt
		snapshot.NextBeforeConfirmationID = last.ID
	}

	if err = tx.Commit(ctx); err != nil {
		return application.ExecutionGovernanceSnapshot{}, application.ErrUnavailable
	}
	return snapshot, nil
}

func (r *ExecutionGovernanceRepository) CreateExecutionPolicy(ctx context.Context, workspace, id, actor, maxUnconfirmed, maxMachine string, denyUnsafe bool, ttlSeconds int, at time.Time) (application.ExecutionGovernancePolicyRevision, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.ExecutionGovernancePolicyRevision{}, err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT governance.create_execution_policy($1,$2,$3,$4,$5,$6,$7,$8)`, workspace, id, actor, maxUnconfirmed, maxMachine, denyUnsafe, ttlSeconds, at); err != nil {
		return application.ExecutionGovernancePolicyRevision{}, mapErr(err)
	}
	item, err := scanExecutionGovernancePolicy(tx.QueryRow(ctx, `SELECT `+executionGovernancePolicyCols+` FROM governance.execution_policy_revisions WHERE workspace_id=$1 AND id=$2`, workspace, id))
	if err != nil {
		return application.ExecutionGovernancePolicyRevision{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ExecutionGovernancePolicyRevision{}, application.ErrUnavailable
	}
	return item, nil
}

func (r *ExecutionGovernanceRepository) ActivateExecutionPolicy(ctx context.Context, workspace, id, actor string, at time.Time) (application.ExecutionGovernancePolicyRevision, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.ExecutionGovernancePolicyRevision{}, err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT governance.activate_execution_policy($1,$2,$3,$4)`, workspace, id, actor, at); err != nil {
		return application.ExecutionGovernancePolicyRevision{}, mapErr(err)
	}
	item, err := scanExecutionGovernancePolicy(tx.QueryRow(ctx, `SELECT `+executionGovernancePolicyCols+` FROM governance.execution_policy_revisions WHERE workspace_id=$1 AND id=$2`, workspace, id))
	if err != nil {
		return application.ExecutionGovernancePolicyRevision{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ExecutionGovernancePolicyRevision{}, application.ErrUnavailable
	}
	return item, nil
}

var _ application.ExecutionGovernanceRepository = (*ExecutionGovernanceRepository)(nil)
