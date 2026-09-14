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

func (r *Repository) FindOAuthRefreshTarget(ctx context.Context, workspace, provider string, before time.Time) (application.OAuthRefreshTarget, error) {
	if r == nil || r.pool == nil || workspace == "" || provider == "" || before.IsZero() {
		return application.OAuthRefreshTarget{}, application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return application.OAuthRefreshTarget{}, application.ErrUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", workspace); err != nil {
		return application.OAuthRefreshTarget{}, application.ErrUnavailable
	}
	var target application.OAuthRefreshTarget
	err = tx.QueryRow(ctx, `SELECT c.workspace_id,c.id,c.provider_id,c.credential_version_ref,c.revision,c.expires_at,
	 s.refresh_credential_ref,s.refresh_secret_revision,s.required_scopes,s.granted_scopes
	 FROM connections.connections c JOIN connections.oauth_refresh_sessions s ON (s.workspace_id,s.connection_id)=(c.workspace_id,c.id)
	 WHERE c.workspace_id=$1 AND c.provider_id=$2 AND c.state='active' AND s.state='active' AND c.revision=s.connection_revision AND c.expires_at<=$3
	 ORDER BY c.expires_at,c.id LIMIT 1`, workspace, provider, before).Scan(
		&target.WorkspaceID, &target.ConnectionID, &target.ProviderID, &target.AccessCredentialRef, &target.ConnectionRevision, &target.ExpiresAt,
		&target.RefreshCredentialRef, &target.RefreshSecretRevision, &target.RequiredScopes, &target.GrantedScopes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return application.OAuthRefreshTarget{}, application.ErrNoOAuthRefreshCandidate
	}
	if err != nil || target.WorkspaceID != workspace || target.ProviderID != provider || target.ConnectionRevision < 1 || target.RefreshSecretRevision < 1 || len(target.RequiredScopes) < 1 || len(target.GrantedScopes) < 1 {
		return application.OAuthRefreshTarget{}, application.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return application.OAuthRefreshTarget{}, application.ErrUnavailable
	}
	return target, nil
}

func (r *Repository) CommitOAuthRefresh(ctx context.Context, input application.OAuthRefreshCommit) (domain.Summary, bool, error) {
	t := input.Target
	if r == nil || r.pool == nil || t.WorkspaceID == "" || t.ConnectionID == "" || input.AccessCredentialRef == "" || input.RefreshCredentialRef == "" || input.RefreshSecretRevision < 1 || len(input.GrantedScopes) < 1 || input.RefreshedAt.IsZero() || !input.ExpiresAt.After(input.RefreshedAt) {
		return domain.Summary{}, false, application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Summary{}, false, application.ErrUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", t.WorkspaceID); err != nil {
		return domain.Summary{}, false, application.ErrUnavailable
	}
	var item domain.Summary
	err = tx.QueryRow(ctx, `UPDATE connections.connections SET credential_version_ref=$5,expires_at=$6,revision=revision+1
	 WHERE workspace_id=$1 AND id=$2 AND provider_id=$3 AND state='active' AND revision=$4
	 RETURNING workspace_id,id,provider_id,state,revision,created_at,expires_at`, t.WorkspaceID, t.ConnectionID, t.ProviderID, t.ConnectionRevision, input.AccessCredentialRef, input.ExpiresAt).Scan(&item.WorkspaceID, &item.ConnectionID, &item.ProviderID, &item.State, &item.Revision, &item.CreatedAt, &item.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Summary{}, false, nil
	}
	if err != nil || item.Validate() != nil {
		return domain.Summary{}, false, application.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, `UPDATE connections.oauth_refresh_sessions SET refresh_credential_ref=$5,refresh_secret_revision=$6,connection_revision=$7,granted_scopes=$8,state='active',last_error_code=NULL,updated_at=$9,last_refreshed_at=$9
	 WHERE workspace_id=$1 AND connection_id=$2 AND provider_id=$3 AND state='active' AND connection_revision=$4`, t.WorkspaceID, t.ConnectionID, t.ProviderID, t.ConnectionRevision, input.RefreshCredentialRef, input.RefreshSecretRevision, item.Revision, input.GrantedScopes, input.RefreshedAt)
	if err != nil || tag.RowsAffected() != 1 {
		return domain.Summary{}, false, application.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `UPDATE connections.connection_grants SET expires_at=$3 WHERE workspace_id=$1 AND connection_id=$2 AND active IS TRUE AND expires_at=$4`, t.WorkspaceID, t.ConnectionID, input.ExpiresAt, t.ExpiresAt); err != nil {
		return domain.Summary{}, false, application.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Summary{}, false, application.ErrUnavailable
	}
	return item, true, nil
}

func (r *Repository) FailOAuthRefresh(ctx context.Context, target application.OAuthRefreshTarget, code string, at time.Time) (domain.Summary, bool, error) {
	if r == nil || r.pool == nil || target.WorkspaceID == "" || target.ConnectionID == "" || at.IsZero() || code != "invalid_grant" && code != "scope_reduced" && code != "invalid_token" {
		return domain.Summary{}, false, application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Summary{}, false, application.ErrUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", target.WorkspaceID); err != nil {
		return domain.Summary{}, false, application.ErrUnavailable
	}
	var item domain.Summary
	err = tx.QueryRow(ctx, `UPDATE connections.connections SET state='error',revision=revision+1
	 WHERE workspace_id=$1 AND id=$2 AND provider_id=$3 AND state='active' AND revision=$4
	 RETURNING workspace_id,id,provider_id,state,revision,created_at,expires_at`, target.WorkspaceID, target.ConnectionID, target.ProviderID, target.ConnectionRevision).Scan(&item.WorkspaceID, &item.ConnectionID, &item.ProviderID, &item.State, &item.Revision, &item.CreatedAt, &item.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Summary{}, false, nil
	}
	if err != nil || item.Validate() != nil {
		return domain.Summary{}, false, application.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, `UPDATE connections.oauth_refresh_sessions SET state='error',connection_revision=$5,last_error_code=$6,updated_at=$7
	 WHERE workspace_id=$1 AND connection_id=$2 AND provider_id=$3 AND state='active' AND connection_revision=$4`, target.WorkspaceID, target.ConnectionID, target.ProviderID, target.ConnectionRevision, item.Revision, code, at)
	if err != nil || tag.RowsAffected() != 1 {
		return domain.Summary{}, false, application.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Summary{}, false, application.ErrUnavailable
	}
	return item, true, nil
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
		previousRevision := item.Revision
		if at.IsZero() || at.Before(item.CreatedAt) {
			return domain.Summary{}, application.ErrForbidden
		}
		err = tx.QueryRow(ctx, `UPDATE connections.connections SET state='revoked',revision=revision+1 WHERE workspace_id=$1 AND id=$2 RETURNING workspace_id,id,provider_id,state,revision,created_at,expires_at`, workspace, connectionID).Scan(&item.WorkspaceID, &item.ConnectionID, &item.ProviderID, &item.State, &item.Revision, &item.CreatedAt, &item.ExpiresAt)
		if err != nil {
			return domain.Summary{}, application.ErrUnavailable
		}
		if _, err = tx.Exec(ctx, `SELECT connections.revoke_oauth_refresh_session($1,$2,$3,$4,$5)`, workspace, connectionID, previousRevision, item.Revision, at); err != nil {
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
	if input.RefreshCredentialRef != "" {
		if input.RefreshSecretRevision < 1 || len(input.RequiredScopes) < 1 || len(input.GrantedScopes) < 1 {
			return domain.Summary{}, application.ErrUnavailable
		}
		if _, err = tx.Exec(ctx, `INSERT INTO connections.oauth_refresh_sessions(
		 workspace_id,connection_id,provider_id,refresh_credential_ref,refresh_secret_revision,connection_revision,required_scopes,granted_scopes,state,last_error_code,created_at,updated_at,last_refreshed_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,'active',NULL,$9,$9,NULL)`, input.WorkspaceID, input.ConnectionID, input.ProviderID, input.RefreshCredentialRef, input.RefreshSecretRevision, input.Revision, input.RequiredScopes, input.GrantedScopes, input.CreatedAt); err != nil {
			return domain.Summary{}, application.ErrUnavailable
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Summary{}, application.ErrUnavailable
	}
	return item, nil
}

var _ application.Repository = (*Repository)(nil)
var _ application.HumanRepository = (*Repository)(nil)
var _ application.OAuthRepository = (*Repository)(nil)
var _ application.OAuthRefreshRepository = (*Repository)(nil)
