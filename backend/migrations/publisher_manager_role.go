package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantPublisherManager provisions the invite-only Publisher workbench role.
// It can manage Workspace-scoped Publisher/Plugin drafts and run server-owned
// preflight, but it cannot mutate publication state or inspect execution,
// identity, commerce, catalog internals or supplier credentials/endpoints.
func GrantPublisherManager(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid publisher-manager role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("publisher-manager role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("publisher-manager grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA supply,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT SELECT ON supply.publishers,supply.plugins,supply.plugin_versions TO " + id,
		"GRANT INSERT (workspace_id,id,owner_user_id,display_name) ON supply.publishers TO " + id,
		"GRANT UPDATE (display_name) ON supply.publishers TO " + id,
		"GRANT INSERT (workspace_id,id,publisher_id,created_by_user_id) ON supply.plugins TO " + id,
		"GRANT INSERT (workspace_id,plugin_id,version,publisher_id,manifest_json,manifest_sha256,created_by_user_id) ON supply.plugin_versions TO " + id,
		"GRANT UPDATE (manifest_json,manifest_sha256) ON supply.plugin_versions TO " + id,
		"GRANT DELETE ON supply.plugin_versions TO " + id,
		"GRANT EXECUTE ON FUNCTION supply.plugin_version_publish_issues(text,text,text) TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("publisher-manager grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("publisher-manager grant commit failed")
	}
	return nil
}
