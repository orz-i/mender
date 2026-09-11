package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ExecutorRole is deliberately separate from the Worker lease-control role.
// It can assemble an invocation from immutable admitted input, an opaque
// connection credential reference and a Supply deployment descriptor, but it
// has no secret store capability and no business-table write permission.
func ExecutorRole(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("executor database unavailable")
	}
	var unsafe bool
	err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication))
	 OR EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','supply','mender_meta') AND (pg_has_role(current_user,n.nspowner,'MEMBER') OR has_schema_privilege(current_user,n.oid,'CREATE')))
	 OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','supply','mender_meta') AND c.relkind IN('r','p') AND pg_has_role(current_user,c.relowner,'MEMBER'))
	 OR has_database_privilege(current_user,current_database(),'CREATE')`).Scan(&unsafe)
	if err != nil || unsafe {
		return errors.New("executor role is privileged")
	}
	var ok bool
	err = pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,'execution','USAGE')
	 AND has_schema_privilege(current_user,'connections','USAGE')
	 AND has_schema_privilege(current_user,'supply','USAGE')
	 AND has_schema_privilege(current_user,'mender_meta','USAGE')
	 AND has_table_privilege(current_user,'mender_meta.schema_migrations','SELECT')
	 AND has_table_privilege(current_user,'supply.deployments','SELECT')
	 AND has_column_privilege(current_user,'execution.run_admissions','workspace_id','SELECT')
	 AND has_column_privilege(current_user,'execution.run_admissions','run_id','SELECT')
	 AND has_column_privilege(current_user,'execution.run_admissions','subject_id','SELECT')
	 AND has_column_privilege(current_user,'execution.run_admissions','connection_id','SELECT')
	 AND has_column_privilege(current_user,'execution.run_admissions','tool_version_id','SELECT')
	 AND has_column_privilege(current_user,'execution.run_admissions','deployment_revision','SELECT')
	 AND has_column_privilege(current_user,'execution.run_admissions','canonical_arguments','SELECT')
	 AND has_column_privilege(current_user,'connections.connections','credential_version_ref','SELECT')
	 AND has_column_privilege(current_user,'connections.connection_grants','subject_id','SELECT')`).Scan(&ok)
	if err != nil || !ok {
		return errors.New("executor role lacks required read access")
	}
	for _, schema := range []string{"identity", "commerce", "catalog", "distribution"} {
		if err = pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,$1,'USAGE')`, schema).Scan(&unsafe); err != nil || unsafe {
			return errors.New("executor role can reach unrelated business schema")
		}
	}
	for _, column := range []string{"credential_id", "idempotency_key", "request_hash", "reservation_id", "toolset_version_id", "price_version_id", "budget_id", "period_id", "currency", "reserved_micro", "created_at"} {
		if err = pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'execution.run_admissions',$1,'SELECT')`, column).Scan(&unsafe); err != nil || unsafe {
			return errors.New("executor role exposes unrelated admission data")
		}
	}
	// Cross-schema reachability above is authoritative for identity/commerce/
	// catalog/distribution. Within the schemas this role can actually USE, reject
	// access to unrelated execution tables explicitly.
	for _, table := range []string{"execution.runs", "execution.jobs", "execution.run_attempts", "execution.run_events", "execution.outbox", "execution.run_cancellations", "execution.provider_observations", "execution.provider_cancel_intents"} {
		if err = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$1,'SELECT,INSERT,UPDATE')`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("executor role has unrelated table access: " + table)
		}
	}
	for _, table := range []string{"execution.run_admissions", "connections.connections", "connections.connection_grants", "supply.deployments", "mender_meta.schema_migrations"} {
		if err = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$1,'INSERT,UPDATE')`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("executor role can write invocation source data")
		}
	}
	var rls int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relrowsecurity AND c.relforcerowsecurity AND ((n.nspname='execution' AND c.relname='run_admissions') OR (n.nspname='connections' AND c.relname IN('connections','connection_grants')))`).Scan(&rls); err != nil || rls != 3 {
		return errors.New("executor RLS safeguards missing")
	}
	return nil
}
