package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantMCPConnector grants the narrow read plane needed to assemble an
// admitted invocation plus append/select permissions for Supply-owned MCP
// discovery/result evidence. It cannot mutate Connection, Execution or
// Deployment authority facts and never receives secret values from PostgreSQL.
func GrantMCPConnector(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid MCP connector role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("MCP connector role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("MCP connector grant unavailable")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA execution,connections,supply,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations,supply.deployments,supply.mcp_tool_routes TO " + id,
		"GRANT EXECUTE ON FUNCTION supply.provider_accepts_new_work(text) TO " + id,
		"GRANT SELECT,INSERT ON supply.mcp_tool_snapshots,supply.mcp_call_results TO " + id,
		"GRANT SELECT (workspace_id,run_id,subject_id,connection_id,tool_version_id,deployment_revision,canonical_arguments) ON execution.run_admissions TO " + id,
		"GRANT SELECT (workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at) ON connections.connections TO " + id,
		"GRANT SELECT (workspace_id,connection_id,subject_id,active,created_at,expires_at) ON connections.connection_grants TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("MCP connector grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("MCP connector grant commit failed")
	}
	return nil
}
