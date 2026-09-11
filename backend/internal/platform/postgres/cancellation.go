package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CancellationRole is intentionally incompatible with both the API reader and
// admission writer. It may release quota and stop existing jobs, never admit runs.
func CancellationRole(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("cancellation database unavailable")
	}
	var unsafe bool
	e := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication)) OR EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','supply','mender_meta') AND (pg_has_role(current_user,n.nspowner,'MEMBER') OR has_schema_privilege(current_user,n.oid,'CREATE'))) OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','supply','mender_meta') AND c.relkind IN('r','p') AND pg_has_role(current_user,c.relowner,'MEMBER')) OR has_database_privilege(current_user,current_database(),'CREATE')`).Scan(&unsafe)
	if e != nil || unsafe {
		return errors.New("cancellation role is privileged")
	}
	allowed := map[string][]string{
		"commerce.budget_periods": {"reserved_micro", "revision"},
		"commerce.reservations":   {"state", "released_at"},
		"execution.runs":          {"state", "version", "updated_at"},
		"execution.jobs":          {"state", "stopped_at", "updated_at"},
		"execution.outbox":        {"delivery_state"},
	}
	inserts := map[string]bool{"execution.outbox": true, "execution.run_events": true, "execution.run_cancellations": true, "execution.provider_cancel_intents": true}
	rows, e := pool.Query(ctx, `SELECT n.nspname||'.'||c.relname,c.oid::bigint FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','supply','mender_meta') AND c.relkind IN('r','p')`)
	if e != nil {
		return errors.New("cancellation role metadata unavailable")
	}
	tables := map[string]int64{}
	for rows.Next() {
		var name string
		var oid int64
		if e = rows.Scan(&name, &oid); e != nil {
			rows.Close()
			return errors.New("cancellation metadata invalid")
		}
		tables[name] = oid
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return errors.New("cancellation metadata unavailable")
	}
	for name, oid := range tables {
		var bad, canInsert bool
		e = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1::oid,'DELETE,TRUNCATE,TRIGGER,REFERENCES') OR EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=$1::oid AND a.attnum>0 AND NOT a.attisdropped AND has_column_privilege(current_user,$1::oid,a.attnum,'UPDATE') AND NOT(a.attname=ANY(COALESCE($2::text[],ARRAY[]::text[])))),has_any_column_privilege(current_user,$1::oid,'INSERT')`, oid, allowed[name]).Scan(&bad, &canInsert)
		// Nil arrays must represent an empty allowlist, not SQL NULL.
		if e != nil || bad || canInsert != inserts[name] {
			return errors.New("cancellation role has unexpected write access")
		}
		for _, column := range allowed[name] {
			var ok bool
			if e = pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,$1::oid,$2,'UPDATE')`, oid, column).Scan(&ok); e != nil || !ok {
				return errors.New("cancellation role lacks transition permission")
			}
		}
	}
	for _, name := range []string{"commerce.budget_periods", "commerce.reservations", "execution.runs", "execution.jobs", "execution.outbox", "execution.run_events", "execution.run_cancellations", "execution.provider_cancel_intents", "mender_meta.schema_migrations"} {
		var ok bool
		oid, found := tables[name]
		if !found {
			return errors.New("cancellation schema missing")
		}
		if e = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1::oid,'SELECT')`, oid).Scan(&ok); e != nil || !ok {
			return errors.New("cancellation role lacks read permission")
		}
	}
	for _, col := range []string{"workspace_id", "run_id", "reservation_id", "budget_id", "period_id", "currency", "reserved_micro"} {
		var ok bool
		if e = pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,$1::oid,$2,'SELECT')`, tables["execution.run_admissions"], col).Scan(&ok); e != nil || !ok {
			return errors.New("cancellation reference permission missing")
		}
	}
	for _, col := range []string{"canonical_arguments", "credential_id", "idempotency_key", "request_hash"} {
		if e = pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,$1::oid,$2,'SELECT')`, tables["execution.run_admissions"], col).Scan(&unsafe); e != nil || unsafe {
			return errors.New("cancellation role exposes admission input")
		}
	}
	attemptOID, found := tables["execution.run_attempts"]
	if !found {
		return errors.New("cancellation schema missing")
	}
	for _, col := range []string{"workspace_id", "run_id", "attempt_no", "state", "provider_id", "provider_request_id", "external_task_id", "submission_intent_at", "submitted_at"} {
		var ok bool
		if e = pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,$1::oid,$2,'SELECT')`, attemptOID, col).Scan(&ok); e != nil || !ok {
			return errors.New("cancellation attempt-proof permission missing")
		}
	}
	for _, col := range []string{"lease_generation", "lease_owner", "leased_at", "lease_until", "finished_at", "submission_key", "unknown_at", "unknown_reason"} {
		if e = pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,$1::oid,$2,'SELECT')`, attemptOID, col).Scan(&unsafe); e != nil || unsafe {
			return errors.New("cancellation role exposes worker attempt details")
		}
	}
	for _, name := range []string{"catalog.tool_versions", "distribution.toolset_bindings", "connections.connections", "connections.connection_grants", "commerce.price_versions", "supply.deployments"} {
		if e = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1::oid,'SELECT') OR has_any_column_privilege(current_user,$1::oid,'SELECT')`, tables[name]).Scan(&unsafe); e != nil || unsafe {
			return errors.New("cancellation role exposes admission plan data")
		}
	}
	if oid, found := tables["execution.provider_observations"]; !found {
		return errors.New("cancellation schema missing")
	} else if e = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1::oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$1::oid,'SELECT,INSERT,UPDATE')`, oid).Scan(&unsafe); e != nil || unsafe {
		return errors.New("cancellation role exposes provider results")
	}
	if oid, found := tables["execution.provider_cancel_intents"]; !found {
		return errors.New("cancellation schema missing")
	} else {
		for _, col := range []string{"workspace_id", "run_id", "attempt_no", "cancel_key", "provider_id", "provider_request_id", "external_task_id", "requested_by_subject", "requested_by_credential", "reason", "state", "requested_at"} {
			var ok bool
			if e = pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,$1::oid,$2,'INSERT')`, oid, col).Scan(&ok); e != nil || !ok {
				return errors.New("cancellation provider-intent insert permission missing")
			}
		}
		for _, col := range []string{"sending_at", "resolved_at", "outcome_observation_id", "unknown_reason"} {
			if e = pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,$1::oid,$2,'INSERT,UPDATE')`, oid, col).Scan(&unsafe); e != nil || unsafe {
				return errors.New("cancellation role can forge provider cancel outcome")
			}
		}
	}
	var count int
	e = pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relrowsecurity AND c.relforcerowsecurity AND ((n.nspname='execution' AND c.relname IN('runs','run_admissions','jobs','outbox','run_events','run_cancellations','provider_cancel_intents')) OR(n.nspname='commerce' AND c.relname IN('budget_periods','reservations')))`).Scan(&count)
	if e != nil || count != 9 {
		return errors.New("cancellation RLS protection missing")
	}
	return nil
}
