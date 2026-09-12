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

func (r *Repository) ListSummaries(ctx context.Context, workspace string) ([]domain.Summary, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", workspace); err != nil {
		return nil, application.ErrUnavailable
	}
	rows, err := tx.Query(ctx, `SELECT workspace_id,id,provider_id,state,revision,created_at,expires_at FROM connections.connections WHERE workspace_id=$1 ORDER BY created_at DESC,id DESC LIMIT 200`, workspace)
	if err != nil {
		return nil, application.ErrUnavailable
	}
	defer rows.Close()
	items := make([]domain.Summary, 0)
	for rows.Next() {
		var item domain.Summary
		if err = rows.Scan(&item.WorkspaceID, &item.ConnectionID, &item.ProviderID, &item.State, &item.Revision, &item.CreatedAt, &item.ExpiresAt); err != nil || item.Validate() != nil {
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

func (r *Repository) Revoke(ctx context.Context, workspace, connectionID string, at time.Time) (domain.Summary, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Summary{}, application.ErrUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", workspace); err != nil {
		return domain.Summary{}, application.ErrUnavailable
	}
	var item domain.Summary
	err = tx.QueryRow(ctx, `SELECT workspace_id,id,provider_id,state,revision,created_at,expires_at FROM connections.connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspace, connectionID).Scan(&item.WorkspaceID, &item.ConnectionID, &item.ProviderID, &item.State, &item.Revision, &item.CreatedAt, &item.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Summary{}, application.ErrForbidden
	}
	if err != nil || item.Validate() != nil {
		return domain.Summary{}, application.ErrUnavailable
	}
	if item.State != "revoked" {
		if at.IsZero() || at.Before(item.CreatedAt) {
			return domain.Summary{}, application.ErrForbidden
		}
		err = tx.QueryRow(ctx, `UPDATE connections.connections SET state='revoked',revision=revision+1 WHERE workspace_id=$1 AND id=$2 RETURNING workspace_id,id,provider_id,state,revision,created_at,expires_at`, workspace, connectionID).Scan(&item.WorkspaceID, &item.ConnectionID, &item.ProviderID, &item.State, &item.Revision, &item.CreatedAt, &item.ExpiresAt)
		if err != nil {
			return domain.Summary{}, application.ErrUnavailable
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Summary{}, application.ErrUnavailable
	}
	return item, nil
}

func (r *Repository) CreateOAuthConnection(ctx context.Context, input application.NewOAuthConnection) (domain.Summary, error) {
	if r == nil || r.pool == nil || input.WorkspaceID == "" || input.ConnectionID == "" || input.ProviderID == "" || input.CredentialVersionRef == "" || input.SubjectID == "" || input.Revision != 1 || input.CreatedAt.IsZero() || !input.ExpiresAt.After(input.CreatedAt) {
		return domain.Summary{}, application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Summary{}, application.ErrUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", input.WorkspaceID); err != nil {
		return domain.Summary{}, application.ErrUnavailable
	}
	var item domain.Summary
	err = tx.QueryRow(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at) VALUES($1,$2,$3,$4,'active',$5,$6,$7) RETURNING workspace_id,id,provider_id,state,revision,created_at,expires_at`, input.WorkspaceID, input.ConnectionID, input.ProviderID, input.CredentialVersionRef, input.Revision, input.CreatedAt, input.ExpiresAt).Scan(&item.WorkspaceID, &item.ConnectionID, &item.ProviderID, &item.State, &item.Revision, &item.CreatedAt, &item.ExpiresAt)
	if err != nil || item.Validate() != nil {
		return domain.Summary{}, application.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at) VALUES($1,$2,$3,true,$4,$5)`, input.WorkspaceID, input.ConnectionID, input.SubjectID, input.CreatedAt, input.ExpiresAt); err != nil {
		return domain.Summary{}, application.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Summary{}, application.ErrUnavailable
	}
	return item, nil
}

var _ application.Repository = (*Repository)(nil)
var _ application.HumanRepository = (*Repository)(nil)
var _ application.OAuthRepository = (*Repository)(nil)
