package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/processes/consolelaunch/application"
)

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) ListLaunchOptions(ctx context.Context, workspace, userID string, at time.Time) ([]application.LaunchOption, error) {
	if r == nil || r.pool == nil {
		return nil, application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrUnavailable
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", workspace); err != nil {
		return nil, application.ErrUnavailable
	}
	rows, err := tx.Query(ctx, `SELECT b.workspace_id,b.toolset_version_id,b.tool_id,b.tool_version_label,b.tool_version_id,
	 tv.title,tv.description,tv.input_schema::text,tv.side_effect,tv.idempotency,
	 c.id,c.provider_id,p.currency,p.reserve_micro
	 FROM distribution.toolset_bindings b
	 JOIN catalog.tool_versions tv ON tv.id=b.tool_version_id AND tv.tool_id=b.tool_id AND tv.version=b.tool_version_label
	 JOIN connections.connections c ON c.workspace_id=b.workspace_id AND c.id=b.connection_id AND c.provider_id=tv.provider_id
	 JOIN connections.connection_grants g ON g.workspace_id=c.workspace_id AND g.connection_id=c.id AND g.subject_id=$2
	 JOIN commerce.price_versions p ON p.id=tv.price_version_id AND p.tool_version_id=tv.id
	 JOIN commerce.budget_periods bp ON bp.workspace_id=b.workspace_id AND bp.budget_id=b.budget_id AND bp.currency=p.currency
	 WHERE b.workspace_id=$1 AND b.state='published' AND tv.state='published' AND b.connection_id IS NOT NULL
	 AND c.state='active' AND c.expires_at>$3 AND g.active AND g.created_at<=$3 AND g.expires_at>$3
	 AND p.active AND p.starts_at<=$3 AND p.ends_at>$3 AND bp.active AND bp.starts_at<=$3 AND bp.ends_at>$3
	 ORDER BY b.toolset_version_id,tv.title,b.tool_id,c.id LIMIT 500`, workspace, userID, at)
	if err != nil {
		return nil, application.ErrUnavailable
	}
	defer rows.Close()
	items := make([]application.LaunchOption, 0)
	for rows.Next() {
		var item application.LaunchOption
		if err = rows.Scan(&item.WorkspaceID, &item.ToolsetVersionID, &item.ToolID, &item.ToolVersion, &item.ToolVersionID, &item.Title, &item.Description, &item.InputSchema, &item.SideEffect, &item.Idempotency, &item.ConnectionID, &item.ProviderID, &item.Currency, &item.ReserveMicro); err != nil {
			return nil, application.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, application.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, application.ErrUnavailable
	}
	return items, nil
}

var _ application.Repository = (*Repository)(nil)
