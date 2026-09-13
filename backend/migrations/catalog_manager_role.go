package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantCatalogManager provisions the Human Catalog/Toolset management role.
// It intentionally excludes identity, execution, reservation/settlement writes,
// connection secrets, and mutation of immutable published Catalog facts.
func GrantCatalogManager(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid catalog-manager role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("catalog-manager role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("catalog-manager grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA catalog,distribution,connections,commerce,governance,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT SELECT ON catalog.tool_version_management TO " + id,
		"GRANT INSERT (workspace_id,tool_version_id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable) ON catalog.tool_version_management TO " + id,
		"GRANT UPDATE (tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable) ON catalog.tool_version_management TO " + id,
		"GRANT DELETE ON catalog.tool_version_management TO " + id,
		"GRANT SELECT ON distribution.toolsets,distribution.toolset_bindings TO " + id,
		"GRANT INSERT (workspace_id,id) ON distribution.toolsets TO " + id,
		"GRANT INSERT (workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,mcp_name,mcp_exposed) ON distribution.toolset_bindings TO " + id,
		"GRANT UPDATE (tool_id,tool_version_label,tool_version_id,budget_id,connection_id,mcp_name,mcp_exposed) ON distribution.toolset_bindings TO " + id,
		"GRANT DELETE ON distribution.toolset_bindings TO " + id,
		"GRANT SELECT (workspace_id,id,provider_id,state,revision,created_at,expires_at) ON connections.connections TO " + id,
		"GRANT SELECT (id,tool_version_id,currency,reserve_micro,starts_at,ends_at,active) ON commerce.price_versions TO " + id,
		"GRANT SELECT (workspace_id,budget_id,period_id,currency,starts_at,ends_at,active) ON commerce.budget_periods TO " + id,
		"GRANT SELECT ON governance.catalog_publication_approvals TO " + id,
		"GRANT SELECT ON governance.catalog_publication_policy_decisions TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.submit_catalog_publication(text,text,text,text,text,timestamptz,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.submit_catalog_publication_with_policy(text,text,text,text,text,timestamptz,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION catalog.tool_version_publish_issues(text,text,timestamptz),catalog.publish_tool_version(text,text,timestamptz),catalog.retire_tool_version(text,text,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION distribution.toolset_publish_issues(text,text,timestamptz),distribution.publish_toolset(text,text,timestamptz),distribution.retire_toolset(text,text,timestamptz) TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("catalog-manager grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("catalog-manager grant commit failed")
	}
	return nil
}
