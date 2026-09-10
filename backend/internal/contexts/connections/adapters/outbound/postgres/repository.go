package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/connections/application"
	"github.com/orz-i/mender/backend/internal/contexts/connections/domain"
)

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func (r *Repository) FindAccess(ctx context.Context, workspace, subject, connectionID string) (domain.Access, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Access{}, application.ErrUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", workspace); err != nil {
		return domain.Access{}, application.ErrUnavailable
	}
	var a domain.Access
	err = tx.QueryRow(ctx, `SELECT c.workspace_id,c.id,c.provider_id,c.state,c.revision,c.created_at,c.expires_at,g.subject_id,g.active,g.created_at,g.expires_at FROM connections.connections c JOIN connections.connection_grants g ON g.workspace_id=c.workspace_id AND g.connection_id=c.id WHERE c.workspace_id=$1 AND c.id=$2 AND g.subject_id=$3`, workspace, connectionID, subject).Scan(&a.WorkspaceID, &a.ConnectionID, &a.ProviderID, &a.State, &a.Revision, &a.ConnectionCreatedAt, &a.ConnectionExpiresAt, &a.SubjectID, &a.GrantActive, &a.GrantCreatedAt, &a.GrantExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Access{}, application.ErrForbidden
	}
	if err != nil {
		return domain.Access{}, application.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Access{}, application.ErrUnavailable
	}
	return a, nil
}

var _ application.Repository = (*Repository)(nil)
