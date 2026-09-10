package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/identity/application"
	"github.com/orz-i/mender/backend/internal/contexts/identity/domain"
)

type Repository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) FindCredential(ctx context.Context, id string) (domain.Credential, error) {
	if !domain.ValidID(id) {
		return domain.Credential{}, application.ErrNotFound
	}
	var c domain.Credential
	err := r.pool.QueryRow(ctx, `SELECT k.id,k.workspace_id,k.subject_id,k.digest,k.scopes,k.created_at,k.expires_at,k.revoked,w.disabled,s.disabled
	 FROM identity.api_keys k JOIN identity.workspaces w ON w.id=k.workspace_id
	 JOIN identity.service_accounts s ON (s.workspace_id,s.id)=(k.workspace_id,k.subject_id) WHERE k.id=$1`, id).Scan(
		&c.ID, &c.WorkspaceID, &c.SubjectID, &c.Digest, &c.Scopes, &c.CreatedAt, &c.ExpiresAt, &c.Revoked, &c.WorkspaceDisabled, &c.SubjectDisabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Credential{}, application.ErrNotFound
	}
	if err != nil {
		return domain.Credential{}, application.ErrUnavailable
	}
	if c.Validate() != nil {
		return domain.Credential{}, application.ErrUnavailable
	}
	return c, nil
}

// Provision is an operator-only adapter. Runtime's database role has no identity writes.
// Existing disabled workspaces/accounts are never re-enabled by issuing another key.
func (r *Repository) Provision(ctx context.Context, c domain.Credential) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Revoked || c.WorkspaceDisabled || c.SubjectDisabled {
		return errors.New("cannot provision inactive credential")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return application.ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err = tx.Exec(ctx, "INSERT INTO identity.workspaces(id) VALUES($1) ON CONFLICT DO NOTHING", c.WorkspaceID); err != nil {
		return application.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, "INSERT INTO identity.service_accounts(workspace_id,id) VALUES($1,$2) ON CONFLICT DO NOTHING", c.WorkspaceID, c.SubjectID); err != nil {
		return application.ErrUnavailable
	}
	var disabled bool
	if err = tx.QueryRow(ctx, "SELECT w.disabled OR s.disabled FROM identity.workspaces w JOIN identity.service_accounts s ON s.workspace_id=w.id WHERE w.id=$1 AND s.id=$2 FOR SHARE OF w,s", c.WorkspaceID, c.SubjectID).Scan(&disabled); err != nil {
		return application.ErrUnavailable
	}
	if disabled {
		return application.ErrForbidden
	}
	if _, err = tx.Exec(ctx, "INSERT INTO identity.api_keys(id,workspace_id,subject_id,digest,scopes,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)", c.ID, c.WorkspaceID, c.SubjectID, c.Digest, c.Scopes, c.CreatedAt, c.ExpiresAt); err != nil {
		return application.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ErrUnavailable
	}
	return nil
}

func (r *Repository) Revoke(ctx context.Context, id string) error {
	if !domain.ValidID(id) {
		return application.ErrNotFound
	}
	tag, err := r.pool.Exec(ctx, "UPDATE identity.api_keys SET revoked=true WHERE id=$1", id)
	if err != nil {
		return application.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return application.ErrNotFound
	}
	return nil
}

var _ application.Repository = (*Repository)(nil)
