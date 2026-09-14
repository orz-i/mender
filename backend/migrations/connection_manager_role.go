package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func GrantConnectionManager(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid connection-manager role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, "SELECT rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb FROM pg_roles WHERE rolname=$1", role).Scan(&elevated); err != nil || elevated {
		return errors.New("connection-manager role must exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("connection-manager grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA connections,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT SELECT (workspace_id,id,provider_id,state,revision,created_at,expires_at) ON connections.connections TO " + id,
		"GRANT INSERT (workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at) ON connections.connections TO " + id,
		"GRANT UPDATE (state,revision) ON connections.connections TO " + id,
		"GRANT INSERT (workspace_id,connection_id,subject_id,active,created_at,expires_at) ON connections.connection_grants TO " + id,
		"GRANT INSERT (workspace_id,connection_id,provider_id,refresh_credential_ref,refresh_secret_revision,connection_revision,required_scopes,granted_scopes,state,last_error_code,created_at,updated_at,last_refreshed_at) ON connections.oauth_refresh_sessions TO " + id,
		"GRANT EXECUTE ON FUNCTION connections.revoke_oauth_refresh_session(text,text,bigint,bigint,timestamptz) TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("connection-manager grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("connection-manager grant commit failed")
	}
	return nil
}
