package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type RuntimeInputs struct{ pool *pgxpool.Pool }

func NewRuntimeInputs(pool *pgxpool.Pool) *RuntimeInputs { return &RuntimeInputs{pool: pool} }

func (r *RuntimeInputs) LoadRuntimeInput(ctx context.Context, workspace domain.WorkspaceID, run domain.RunID) (domain.RuntimeInput, error) {
	if r == nil || r.pool == nil {
		return domain.RuntimeInput{}, application.ErrRuntimeInputUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.RuntimeInput{}, application.ErrRuntimeInputUnavailable
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, string(workspace)); err != nil {
		return domain.RuntimeInput{}, application.ErrRuntimeInputUnavailable
	}
	var input domain.RuntimeInput
	var w, id string
	err = tx.QueryRow(ctx, `SELECT workspace_id,run_id,subject_id,connection_id,tool_version_id,deployment_revision,canonical_arguments FROM execution.run_admissions WHERE workspace_id=$1 AND run_id=$2`, string(workspace), string(run)).Scan(&w, &id, &input.SubjectID, &input.ConnectionID, &input.ToolVersionID, &input.DeploymentRevision, &input.CanonicalArguments)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RuntimeInput{}, application.ErrRuntimeInputNotFound
	}
	if err != nil {
		return domain.RuntimeInput{}, application.ErrRuntimeInputUnavailable
	}
	input.WorkspaceID, input.RunID = domain.WorkspaceID(w), domain.RunID(id)
	if err = tx.Commit(ctx); err != nil {
		return domain.RuntimeInput{}, application.ErrRuntimeInputUnavailable
	}
	return input, nil
}

var _ application.RuntimeInputRepository = (*RuntimeInputs)(nil)
