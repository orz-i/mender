package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

type ReleaseRoutingRepository struct{ pool *pgxpool.Pool }

func NewReleaseRoutingRepository(pool *pgxpool.Pool) *ReleaseRoutingRepository {
	return &ReleaseRoutingRepository{pool: pool}
}

func rollbackReleaseRoute(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func (r *ReleaseRoutingRepository) ResolveReleaseRoute(ctx context.Context, query application.ReleaseRoutingQuery) (application.ReleaseRoutingDecision, error) {
	if r == nil || r.pool == nil {
		return application.ReleaseRoutingDecision{}, application.ErrReleaseRoutingUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return application.ReleaseRoutingDecision{}, application.ErrReleaseRoutingUnavailable
	}
	defer rollbackReleaseRoute(tx)
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, query.WorkspaceID); err != nil {
		return application.ReleaseRoutingDecision{}, application.ErrReleaseRoutingUnavailable
	}
	var value application.ReleaseRoutingDecision
	var deployment, plan *string
	err = tx.QueryRow(ctx, `SELECT deployment_revision,release_plan_id,release_state,release_revision,routed,blocked FROM supply.resolve_release_route($1,$2,$3,$4)`, query.WorkspaceID, query.ToolsetVersionID, query.ToolVersionID, query.DefaultDeploymentRevision).Scan(&deployment, &plan, &value.ReleaseState, &value.ReleaseRevision, &value.Routed, &value.Blocked)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return application.ReleaseRoutingDecision{}, err
		}
		return application.ReleaseRoutingDecision{}, application.ErrReleaseRoutingUnavailable
	}
	if deployment != nil {
		value.DeploymentRevision = *deployment
	}
	if plan != nil {
		value.ReleasePlanID = *plan
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ReleaseRoutingDecision{}, application.ErrReleaseRoutingUnavailable
	}
	return value, nil
}

var _ application.ReleaseRoutingRepository = (*ReleaseRoutingRepository)(nil)
