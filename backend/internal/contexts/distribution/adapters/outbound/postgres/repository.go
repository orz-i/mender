package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/distribution/application"
	"github.com/orz-i/mender/backend/internal/contexts/distribution/domain"
)

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func (r *Repository) FindBinding(ctx context.Context, workspace, toolsetVersionID, toolID, version string) (domain.Binding, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Binding{}, application.ErrUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", workspace); err != nil {
		return domain.Binding{}, application.ErrUnavailable
	}
	var b domain.Binding
	err = tx.QueryRow(ctx, `SELECT workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,state,published_at FROM distribution.toolset_bindings WHERE workspace_id=$1 AND toolset_version_id=$2 AND tool_id=$3 AND tool_version_label=$4`, workspace, toolsetVersionID, toolID, version).Scan(&b.WorkspaceID, &b.ToolsetVersionID, &b.ToolID, &b.ToolVersionLabel, &b.ToolVersionID, &b.BudgetID, &b.State, &b.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Binding{}, application.ErrNotFound
	}
	if err != nil {
		return domain.Binding{}, application.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Binding{}, application.ErrUnavailable
	}
	return b, nil
}

var _ application.Repository = (*Repository)(nil)
