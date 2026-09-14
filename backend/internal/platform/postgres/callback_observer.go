package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func CallbackObserverRole(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("callback-observer database unavailable")
	}
	var unsafe bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("callback-observer database role is privileged")
	}
	for _, column := range []string{"receipt_id", "workspace_id", "provider_id", "event_id", "received_at", "last_received_at", "delivery_count", "run_id", "observation_id", "observation_state", "observed_at", "disposition", "reason_code", "processed_at"} {
		var allowed bool
		if err := pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'execution.provider_callback_inbox',$1,'SELECT')`, column).Scan(&allowed); err != nil || !allowed {
			return errors.New("callback-observer safe projection permission missing")
		}
	}
	for _, column := range []string{"body_sha256", "key_id", "signed_at", "attempt_no", "provider_request_id", "external_task_id"} {
		var allowed bool
		if err := pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'execution.provider_callback_inbox',$1,'SELECT')`, column).Scan(&allowed); err != nil || allowed {
			return errors.New("callback-observer exposes sensitive callback metadata")
		}
	}
	if err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'execution.provider_callback_inbox','INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,'execution.provider_callback_inbox','INSERT,UPDATE')`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("callback-observer can mutate callback Inbox")
	}
	for _, schema := range []string{"identity", "catalog", "distribution", "connections", "commerce", "supply", "governance"} {
		var allowed bool
		if err := pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,$1,'USAGE')`, schema).Scan(&allowed); err != nil || allowed {
			return errors.New("callback-observer can reach unrelated business schema")
		}
	}
	var rls bool
	if err := pool.QueryRow(ctx, `SELECT relrowsecurity AND relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='execution' AND c.relname='provider_callback_inbox'`).Scan(&rls); err != nil || !rls {
		return errors.New("callback-observer RLS safeguard missing")
	}
	return nil
}
