package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WorkerRole validates a dedicated execution-control role. It may persist the
// fenced supplier-submission protocol, but it cannot access supplier credentials
// or any non-execution business schema.
func WorkerRole(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("worker database unavailable")
	}
	var unsafe bool
	err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication))
	 OR EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','mender_meta') AND (pg_has_role(current_user,n.nspowner,'MEMBER') OR has_schema_privilege(current_user,n.oid,'CREATE')))
	 OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','mender_meta') AND c.relkind IN('r','p') AND pg_has_role(current_user,c.relowner,'MEMBER'))
	 OR has_database_privilege(current_user,current_database(),'CREATE')`).Scan(&unsafe)
	if err != nil || unsafe {
		return errors.New("worker role is privileged")
	}
	var ok bool
	err = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'mender_meta.schema_migrations','SELECT')
	 AND has_table_privilege(current_user,'execution.jobs','SELECT')
	 AND has_table_privilege(current_user,'execution.run_attempts','SELECT')
	 AND has_table_privilege(current_user,'execution.runs','SELECT')
	 AND has_column_privilege(current_user,'execution.run_admissions','workspace_id','SELECT')
	 AND has_column_privilege(current_user,'execution.run_admissions','run_id','SELECT')
	 AND has_column_privilege(current_user,'execution.run_admissions','deployment_revision','SELECT')
	 AND has_column_privilege(current_user,'execution.jobs','state','UPDATE')
	 AND has_column_privilege(current_user,'execution.jobs','blocked_reason','UPDATE')
	 AND has_column_privilege(current_user,'execution.jobs','available_at','UPDATE')
	 AND has_column_privilege(current_user,'execution.jobs','lease_owner','UPDATE')
	 AND has_column_privilege(current_user,'execution.jobs','lease_until','UPDATE')
	 AND has_column_privilege(current_user,'execution.jobs','lease_generation','UPDATE')
	 AND has_column_privilege(current_user,'execution.jobs','attempt_count','UPDATE')
	 AND has_column_privilege(current_user,'execution.jobs','updated_at','UPDATE')
	 AND has_table_privilege(current_user,'execution.run_attempts','INSERT')
	 AND has_column_privilege(current_user,'execution.run_attempts','state','UPDATE')
	 AND has_column_privilege(current_user,'execution.run_attempts','lease_until','UPDATE')
	 AND has_column_privilege(current_user,'execution.run_attempts','finished_at','UPDATE')
	 AND has_column_privilege(current_user,'execution.run_attempts','submission_key','UPDATE')
	 AND has_column_privilege(current_user,'execution.run_attempts','submission_intent_at','UPDATE')
	 AND has_column_privilege(current_user,'execution.run_attempts','provider_request_id','UPDATE')
	 AND has_column_privilege(current_user,'execution.run_attempts','external_task_id','UPDATE')
	 AND has_column_privilege(current_user,'execution.run_attempts','submitted_at','UPDATE')
	 AND has_column_privilege(current_user,'execution.run_attempts','unknown_at','UPDATE')
	 AND has_column_privilege(current_user,'execution.run_attempts','unknown_reason','UPDATE')
	 AND has_column_privilege(current_user,'execution.runs','state','UPDATE')
	 AND has_column_privilege(current_user,'execution.runs','version','UPDATE')
	 AND has_column_privilege(current_user,'execution.runs','updated_at','UPDATE')
	 AND has_table_privilege(current_user,'execution.run_events','INSERT')`).Scan(&ok)
	if err != nil || !ok {
		return errors.New("worker role grants are invalid")
	}
	err = pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,'identity','USAGE')
	 OR has_schema_privilege(current_user,'commerce','USAGE')
	 OR has_schema_privilege(current_user,'catalog','USAGE')
	 OR has_schema_privilege(current_user,'distribution','USAGE')
	 OR has_schema_privilege(current_user,'connections','USAGE')`).Scan(&unsafe)
	if err != nil || unsafe {
		return errors.New("worker role can reach non-execution business schemas")
	}
	for _, column := range []string{"credential_id", "idempotency_key", "request_hash", "canonical_arguments", "reservation_id", "price_version_id", "budget_id", "period_id"} {
		if err = pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'execution.run_admissions',$1,'SELECT')`, column).Scan(&unsafe); err != nil || unsafe {
			return errors.New("worker role exposes admission data")
		}
	}
	for _, table := range []string{"execution.outbox", "execution.run_cancellations"} {
		if err = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$1,'SELECT,INSERT,UPDATE')`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("worker role has unrelated data access: " + table)
		}
	}
	if err = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'execution.runs','INSERT,DELETE,TRUNCATE') OR has_column_privilege(current_user,'execution.runs','workspace_id','UPDATE') OR has_column_privilege(current_user,'execution.runs','id','UPDATE') OR has_column_privilege(current_user,'execution.runs','created_at','UPDATE')`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("worker role can rewrite immutable Run fields")
	}
	if err = pool.QueryRow(ctx, `SELECT NOT has_table_privilege(current_user,'execution.run_events','INSERT') OR has_table_privilege(current_user,'execution.run_events','SELECT,UPDATE,DELETE,TRUNCATE')`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("worker event grants are invalid")
	}
	for _, column := range []string{"priority", "max_attempts", "created_at", "stopped_at", "run_id", "workspace_id"} {
		if err = pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'execution.jobs',$1,'UPDATE')`, column).Scan(&unsafe); err != nil || unsafe {
			return errors.New("worker role can rewrite immutable job fields")
		}
	}
	for _, column := range []string{"workspace_id", "run_id", "attempt_no", "lease_generation", "lease_owner", "leased_at"} {
		if err = pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'execution.run_attempts',$1,'UPDATE')`, column).Scan(&unsafe); err != nil || unsafe {
			return errors.New("worker role can rewrite immutable attempt fields")
		}
	}
	var rls int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='execution' AND c.relname IN('runs','run_events','jobs','run_attempts','run_admissions') AND c.relrowsecurity AND c.relforcerowsecurity`).Scan(&rls)
	if err != nil || rls != 5 {
		return errors.New("worker RLS safeguards missing")
	}
	return nil
}
