package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AdmissionRole validates a separate writer; no DDL, identity writes or Run transitions.
func AdmissionRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe bool
	e := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb))
 OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','mender_meta') AND c.relkind='r' AND pg_has_role(current_user,c.relowner,'MEMBER'))
 OR EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','mender_meta') AND (pg_has_role(current_user,n.nspowner,'MEMBER') OR has_schema_privilege(current_user,n.oid,'CREATE')))
 OR has_database_privilege(current_user,current_database(),'CREATE')`).Scan(&unsafe)
	if e != nil || unsafe {
		return errors.New("admission role is privileged")
	}
	if e = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'execution.run_attempts','SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,'execution.run_attempts','SELECT,INSERT,UPDATE')`).Scan(&unsafe); e != nil || unsafe {
		return errors.New("admission writer can access worker attempts")
	}
	var ok bool
	e = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'execution.runs','INSERT') AND NOT has_any_column_privilege(current_user,'execution.runs','UPDATE')
 AND has_table_privilege(current_user,'execution.run_admissions','SELECT') AND has_table_privilege(current_user,'execution.run_admissions','INSERT')
 AND NOT has_table_privilege(current_user,'execution.jobs','INSERT')
 AND has_column_privilege(current_user,'execution.jobs','workspace_id','INSERT')
 AND has_column_privilege(current_user,'execution.jobs','run_id','INSERT')
 AND has_column_privilege(current_user,'execution.jobs','state','INSERT')
 AND has_column_privilege(current_user,'execution.jobs','blocked_reason','INSERT')
 AND has_column_privilege(current_user,'execution.jobs','available_at','INSERT')
 AND has_column_privilege(current_user,'execution.jobs','created_at','INSERT')
 AND has_column_privilege(current_user,'execution.jobs','updated_at','INSERT')
 AND NOT has_column_privilege(current_user,'execution.jobs','priority','INSERT')
 AND NOT has_column_privilege(current_user,'execution.jobs','lease_owner','INSERT')
 AND NOT has_column_privilege(current_user,'execution.jobs','lease_until','INSERT')
 AND NOT has_column_privilege(current_user,'execution.jobs','lease_generation','INSERT')
 AND NOT has_column_privilege(current_user,'execution.jobs','attempt_count','INSERT')
 AND NOT has_column_privilege(current_user,'execution.jobs','max_attempts','INSERT')
 AND NOT has_column_privilege(current_user,'execution.jobs','stopped_at','INSERT')
 AND has_table_privilege(current_user,'execution.outbox','INSERT')
 AND has_table_privilege(current_user,'commerce.budget_periods','SELECT') AND has_column_privilege(current_user,'commerce.budget_periods','reserved_micro','UPDATE') AND has_column_privilege(current_user,'commerce.budget_periods','revision','UPDATE')
 AND has_table_privilege(current_user,'commerce.reservations','SELECT') AND has_table_privilege(current_user,'commerce.reservations','INSERT')
 AND NOT has_table_privilege(current_user,'commerce.budget_periods','INSERT,DELETE,TRUNCATE')
 AND NOT has_column_privilege(current_user,'commerce.budget_periods','limit_micro','UPDATE') AND NOT has_column_privilege(current_user,'commerce.budget_periods','consumed_micro','UPDATE')
 AND NOT has_column_privilege(current_user,'commerce.budget_periods','workspace_id','UPDATE') AND NOT has_column_privilege(current_user,'commerce.budget_periods','budget_id','UPDATE') AND NOT has_column_privilege(current_user,'commerce.budget_periods','period_id','UPDATE')
 AND NOT has_column_privilege(current_user,'commerce.budget_periods','active','UPDATE') AND NOT has_column_privilege(current_user,'commerce.budget_periods','currency','UPDATE') AND NOT has_column_privilege(current_user,'commerce.budget_periods','starts_at','UPDATE') AND NOT has_column_privilege(current_user,'commerce.budget_periods','ends_at','UPDATE')`).Scan(&ok)
	if e != nil || !ok {
		return errors.New("admission role grants are invalid")
	}
	for _, table := range []string{"identity.workspaces", "identity.service_accounts", "identity.api_keys", "mender_meta.schema_migrations"} {
		if e = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,c.oid,'INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,c.oid,'INSERT,UPDATE') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname||'.'||c.relname=$1`, table).Scan(&unsafe); e != nil || unsafe {
			return errors.New("admission role has unrelated write access")
		}
	}
	for _, table := range []string{"catalog.tool_versions", "distribution.toolset_bindings", "connections.connections", "connections.connection_grants", "commerce.price_versions"} {
		if e = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,c.oid,'SELECT,INSERT,UPDATE') FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname||'.'||c.relname=$1`, table).Scan(&unsafe); e != nil || unsafe {
			return errors.New("admission writer can access plan source tables")
		}
	}
	for _, table := range []string{"execution.runs", "execution.run_admissions", "execution.jobs", "execution.outbox", "commerce.reservations"} {
		if e = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$1,'UPDATE')`, table).Scan(&unsafe); e != nil || unsafe {
			return errors.New("admission facts must be append-only")
		}
	}
	var count int
	e = pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relrowsecurity AND c.relforcerowsecurity AND ((n.nspname='execution' AND c.relname IN('runs','run_admissions','jobs','outbox')) OR (n.nspname='commerce' AND c.relname IN('budget_periods','reservations')))`).Scan(&count)
	if e != nil || count != 6 {
		return errors.New("admission RLS protection missing")
	}
	return nil
}
