package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantPlatformAdminManager exposes only reviewed Platform Admin functions.
// The role cannot read or mutate tenant identity, runtime, connection, finance,
// provider deployment, JIT or dangerous-operation tables directly.
func GrantPlatformAdminManager(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid platform-admin-manager role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("platform-admin-manager role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("platform-admin-manager grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA governance,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.platform_admin_workspaces(text) TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.platform_admin_set_workspace_frozen(text,bigint,boolean,text,text,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.platform_admin_providers(text) TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.platform_admin_set_provider_state(text,bigint,text,text,text,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.platform_admin_open_incident(text,text,text,text,text,text,text,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.platform_admin_resolve_incident(text,bigint,text,text,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.platform_admin_incidents(text,text,text,text,timestamptz,text,integer) TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.platform_admin_audit_export(text,bigint,integer) TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("platform-admin-manager grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("platform-admin-manager grant commit failed")
	}
	return nil
}
