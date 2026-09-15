package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CallbackIngestorRole refuses broad principals. The role is intentionally
// close to reconciler authority plus Inbox metadata writes, but still cannot
// inspect admission payloads or any credential/business-owner schema.
func CallbackIngestorRole(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("callback-ingestor database is unavailable")
	}
	var elevated, ok bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=current_user`).Scan(&elevated); err != nil || elevated {
		return errors.New("callback-ingestor database role is privileged")
	}
	for _, schema := range []string{"identity", "commerce", "connections", "supply", "catalog", "distribution"} {
		if err := pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,$1,'USAGE')`, schema).Scan(&ok); err != nil || ok {
			return errors.New("callback-ingestor role can reach unrelated business schema")
		}
	}
	for _, table := range []string{"execution.run_admissions", "execution.outbox", "execution.run_cancellations"} {
		var unsafe bool
		if err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$1,'SELECT,INSERT,UPDATE')`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("callback-ingestor role has unrelated execution access")
		}
	}
	if err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'execution.provider_callback_inbox','SELECT,INSERT')
	 AND has_column_privilege(current_user,'execution.provider_callback_inbox','disposition','UPDATE')
	 AND has_table_privilege(current_user,'execution.provider_observations','SELECT,INSERT')
	 AND has_table_privilege(current_user,'execution.run_attempts','SELECT')
	 AND has_table_privilege(current_user,'execution.runs','SELECT')
	 AND has_table_privilege(current_user,'execution.jobs','SELECT')
	 AND has_table_privilege(current_user,'execution.artifacts','SELECT,INSERT')
	 AND has_table_privilege(current_user,'execution.agent_input_requests','SELECT,INSERT')`).Scan(&ok); err != nil || !ok {
		return errors.New("callback-ingestor required grants are missing")
	}
	for _, table := range []string{"execution.run_attempts", "execution.provider_observations", "execution.run_events", "execution.artifacts", "execution.agent_input_requests"} {
		var unsafe bool
		if err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$1,'UPDATE')`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("callback-ingestor immutable execution facts are mutable")
		}
	}
	var rls int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='execution' AND c.relname IN('runs','jobs','run_attempts','provider_observations','provider_cancel_intents','settlement_jobs','artifacts','provider_callback_inbox','agent_input_requests') AND c.relrowsecurity AND c.relforcerowsecurity`).Scan(&rls); err != nil || rls != 9 {
		return errors.New("callback-ingestor RLS safeguards missing")
	}
	return nil
}
