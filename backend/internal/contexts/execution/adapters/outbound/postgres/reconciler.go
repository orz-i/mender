package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type ProviderReconciliation struct{ pool *pgxpool.Pool }

func NewProviderReconciliation(pool *pgxpool.Pool) *ProviderReconciliation {
	return &ProviderReconciliation{pool: pool}
}

// NextProviderTarget intentionally does not keep a database transaction open
// across the remote status query. Duplicate concurrent polls are acceptable;
// append-only observation idempotency serializes durable convergence later.
func (r *ProviderReconciliation) NextProviderTarget(ctx context.Context, workspace domain.WorkspaceID) (application.ProviderTarget, bool, error) {
	if r == nil || r.pool == nil || !workspace.IsValid() {
		return application.ProviderTarget{}, false, application.ErrProviderResultUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return application.ProviderTarget{}, false, application.ErrProviderResultUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, string(workspace)); err != nil {
		return application.ProviderTarget{}, false, application.ErrProviderResultUnavailable
	}
	var target application.ProviderTarget
	var workspaceID, runID string
	var attemptNo int32
	var externalTask *string
	err = tx.QueryRow(ctx, `SELECT a.workspace_id,a.run_id,a.attempt_no,a.provider_id,a.provider_request_id,a.external_task_id
 FROM execution.jobs j
 JOIN execution.runs r ON (r.workspace_id,r.id)=(j.workspace_id,j.run_id)
 JOIN execution.run_attempts a ON (a.workspace_id,a.run_id,a.lease_generation)=(j.workspace_id,j.run_id,j.lease_generation)
 WHERE j.workspace_id=$1
   AND j.state IN ('provider_waiting','reconciling')
   AND r.state IN ('running','reconciling')
   AND a.state IN ('submitted','unknown')
   AND a.provider_id IS NOT NULL AND a.provider_request_id IS NOT NULL
   AND NOT EXISTS(SELECT 1 FROM execution.provider_observations p WHERE p.workspace_id=j.workspace_id AND p.run_id=j.run_id AND p.state IN ('succeeded','failed','canceled'))
 ORDER BY COALESCE((SELECT max(p.observed_at) FROM execution.provider_observations p WHERE p.workspace_id=j.workspace_id AND p.run_id=j.run_id),j.updated_at),j.run_id
 LIMIT 1`, string(workspace)).Scan(&workspaceID, &runID, &attemptNo, &target.ProviderID, &target.ProviderRequestID, &externalTask)
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			return application.ProviderTarget{}, false, application.ErrProviderResultUnavailable
		}
		return application.ProviderTarget{}, false, nil
	}
	if err != nil {
		return application.ProviderTarget{}, false, application.ErrProviderResultUnavailable
	}
	if attemptNo < 1 {
		return application.ProviderTarget{}, false, application.ErrProviderResultUnavailable
	}
	target.WorkspaceID, target.RunID, target.AttemptNo = domain.WorkspaceID(workspaceID), domain.RunID(runID), uint32(attemptNo)
	if externalTask != nil {
		target.ExternalTaskID = *externalTask
	}
	if !target.Valid() {
		return application.ProviderTarget{}, false, application.ErrProviderResultUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ProviderTarget{}, false, application.ErrProviderResultUnavailable
	}
	return target, true, nil
}

var _ application.ProviderReconciliationRepository = (*ProviderReconciliation)(nil)
