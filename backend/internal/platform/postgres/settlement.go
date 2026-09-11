package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func SettlementRole(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("settlement database unavailable")
	}
	var unsafe bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication))
	 OR EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','supply','mender_meta') AND (pg_has_role(current_user,n.nspowner,'MEMBER') OR has_schema_privilege(current_user,n.oid,'CREATE')))
	 OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','supply','mender_meta') AND c.relkind IN('r','p') AND pg_has_role(current_user,c.relowner,'MEMBER'))
	 OR has_database_privilege(current_user,current_database(),'CREATE')`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("settlement role is privileged")
	}
	for _, schema := range []string{"identity", "catalog", "distribution", "connections", "supply"} {
		if err := pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,$1,'USAGE')`, schema).Scan(&unsafe); err != nil || unsafe {
			return errors.New("settlement role can reach unrelated business schema")
		}
	}
	var ok bool
	if err := pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,'execution','USAGE')
	 AND has_schema_privilege(current_user,'commerce','USAGE')
	 AND has_schema_privilege(current_user,'mender_meta','USAGE')
	 AND has_table_privilege(current_user,'mender_meta.schema_migrations','SELECT')
	 AND has_table_privilege(current_user,'execution.settlement_jobs','SELECT')
	 AND has_column_privilege(current_user,'execution.settlement_jobs','state','UPDATE')
	 AND has_column_privilege(current_user,'execution.settlement_jobs','finished_at','UPDATE')
	 AND has_table_privilege(current_user,'commerce.price_versions','SELECT')
	 AND has_table_privilege(current_user,'commerce.usage_settlements','SELECT,INSERT')
	 AND has_column_privilege(current_user,'commerce.budget_periods','consumed_micro','UPDATE')
	 AND has_column_privilege(current_user,'commerce.budget_periods','reserved_micro','UPDATE')
	 AND has_column_privilege(current_user,'commerce.budget_periods','revision','UPDATE')
	 AND has_column_privilege(current_user,'commerce.reservations','state','UPDATE')
	 AND has_column_privilege(current_user,'commerce.reservations','charged_micro','UPDATE')
	 AND has_column_privilege(current_user,'commerce.reservations','settled_at','UPDATE')`).Scan(&ok); err != nil || !ok {
		return errors.New("settlement role grants are invalid")
	}
	for _, column := range []string{"workspace_id", "run_id", "reservation_id", "price_version_id", "budget_id", "period_id", "currency", "reserved_micro"} {
		if err := pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'execution.run_admissions',$1,'SELECT')`, column).Scan(&ok); err != nil || !ok {
			return errors.New("settlement admission reference permission missing")
		}
	}
	for _, column := range []string{"workspace_id", "run_id", "observation_id", "state", "observed_at"} {
		if err := pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'execution.provider_observations',$1,'SELECT')`, column).Scan(&ok); err != nil || !ok {
			return errors.New("settlement provider fact permission missing")
		}
	}
	for _, column := range []string{"canonical_arguments", "subject_id", "credential_id", "connection_id", "tool_version_id", "toolset_version_id", "deployment_revision", "idempotency_key", "request_hash"} {
		if err := pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'execution.run_admissions',$1,'SELECT')`, column).Scan(&unsafe); err != nil || unsafe {
			return errors.New("settlement role exposes unrelated admission data")
		}
	}
	for _, table := range []string{"execution.runs", "execution.jobs", "execution.run_attempts", "execution.run_events", "execution.outbox", "execution.run_cancellations", "execution.provider_cancel_intents"} {
		if err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$1,'SELECT,INSERT,UPDATE')`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("settlement role has unrelated execution access")
		}
	}
	for _, table := range []string{"execution.run_admissions", "execution.provider_observations"} {
		if err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$1,'INSERT,UPDATE')`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("settlement role can mutate execution evidence")
		}
	}
	if err := pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'execution.settlement_jobs','workspace_id','UPDATE') OR has_column_privilege(current_user,'execution.settlement_jobs','run_id','UPDATE') OR has_column_privilege(current_user,'execution.settlement_jobs','observation_id','UPDATE') OR has_table_privilege(current_user,'execution.settlement_jobs','INSERT,DELETE,TRUNCATE')`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("settlement job identity is mutable")
	}
	var rls int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relrowsecurity AND c.relforcerowsecurity AND ((n.nspname='execution' AND c.relname IN('settlement_jobs','run_admissions','provider_observations')) OR (n.nspname='commerce' AND c.relname IN('budget_periods','reservations','usage_settlements')))`).Scan(&rls); err != nil || rls != 6 {
		return errors.New("settlement RLS safeguards missing")
	}
	return nil
}
