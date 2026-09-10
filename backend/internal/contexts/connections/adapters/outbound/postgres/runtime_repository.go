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

type RuntimeRepository struct{ pool *pgxpool.Pool }

func NewRuntimeRepository(pool *pgxpool.Pool) *RuntimeRepository {
	return &RuntimeRepository{pool: pool}
}

func (r *RuntimeRepository) FindRuntimeCredential(ctx context.Context, workspace, subject, connectionID string) (domain.RuntimeCredential, error) {
	if r == nil || r.pool == nil {
		return domain.RuntimeCredential{}, application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.RuntimeCredential{}, application.ErrUnavailable
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		return domain.RuntimeCredential{}, application.ErrUnavailable
	}
	var value domain.RuntimeCredential
	err = tx.QueryRow(ctx, `SELECT c.workspace_id,c.id,c.provider_id,c.credential_version_ref,c.state,c.revision,c.created_at,c.expires_at,g.subject_id,g.active,g.created_at,g.expires_at FROM connections.connections c JOIN connections.connection_grants g ON g.workspace_id=c.workspace_id AND g.connection_id=c.id WHERE c.workspace_id=$1 AND c.id=$2 AND g.subject_id=$3`, workspace, connectionID, subject).Scan(&value.WorkspaceID, &value.ConnectionID, &value.ProviderID, &value.CredentialVersionRef, &value.State, &value.Revision, &value.ConnectionCreatedAt, &value.ConnectionExpiresAt, &value.SubjectID, &value.GrantActive, &value.GrantCreatedAt, &value.GrantExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RuntimeCredential{}, application.ErrForbidden
	}
	if err != nil {
		return domain.RuntimeCredential{}, application.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.RuntimeCredential{}, application.ErrUnavailable
	}
	return value, nil
}

var _ application.RuntimeRepository = (*RuntimeRepository)(nil)
