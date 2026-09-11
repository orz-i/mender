package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ReconcilerRole(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("reconciler database unavailable")
	}
	var unsafe bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication))
	 OR EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','supply','mender_meta') AND (pg_has_role(current_user,n.nspowner,'MEMBER') OR has_schema_privilege(current_user,n.oid,'CREATE')))
	 OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','supply','mender_meta') AND c.relkind IN('r','p') AND pg_has_role(current_user,c.relowner,'MEMBER'))
	 OR has_database_privilege(current_user,current_database(),'CREATE')`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("reconciler role is privileged")
	}
	for _, schema := range []string{"identity", "commerce", "catalog", "distribution", "connections", "supply"} {
		if err := pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,$1,'USAGE')`, schema).Scan(&unsafe); err != nil || unsafe {
			return errors.New("reconciler role can reach unrelated business schema")
		}
	}
	var ok bool
	if err := pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,'execution','USAGE')
	 AND has_schema_privilege(current_user,'mender_meta','USAGE')
	 AND has_table_privilege(current_user,'mender_meta.schema_migrations','SELECT')
	 AND has_table_privilege(current_user,'execution.runs','SELECT')
	 AND has_table_privilege(current_user,'execution.jobs','SELECT')
	 AND has_table_privilege(current_user,'execution.run_attempts','SELECT')
	 AND has_table_privilege(current_user,'execution.provider_observations','SELECT,INSERT')
	 AND has_table_privilege(current_user,'execution.run_events','INSERT')
	 AND has_column_privilege(current_user,'execution.runs','state','UPDATE')
	 AND has_column_privilege(current_user,'execution.runs','version','UPDATE')
	 AND has_column_privilege(current_user,'execution.runs','updated_at','UPDATE')
	 AND has_column_privilege(current_user,'execution.jobs','state','UPDATE')
	 AND has_column_privilege(current_user,'execution.jobs','blocked_reason','UPDATE')
	 AND has_column_privilege(current_user,'execution.jobs','updated_at','UPDATE')
	 AND has_column_privilege(current_user,'execution.jobs','stopped_at','UPDATE')`).Scan(&ok); err != nil || !ok {
		return errors.New("reconciler role grants are invalid")
	}
	for _, table := range []string{"execution.run_admissions", "execution.outbox", "execution.run_cancellations"} {
		if err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$1,'SELECT,INSERT,UPDATE')`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("reconciler role has unrelated execution access")
		}
	}
	for _, table := range []string{"execution.run_attempts", "execution.provider_observations", "execution.run_events"} {
		if err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$1,'UPDATE')`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("reconciler append-only facts are mutable")
		}
	}
	var rls int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='execution' AND c.relname IN('runs','jobs','run_attempts','provider_observations') AND c.relrowsecurity AND c.relforcerowsecurity`).Scan(&rls); err != nil || rls != 4 {
		return errors.New("reconciler RLS safeguards missing")
	}
	return nil
}
