package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantWorker provisions only the execution-control role. It cannot read
// admission arguments/identity/commerce/connection secrets; Run mutation is
// limited to fenced submission-state transitions protected by database bundles.
func GrantWorker(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid worker role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("worker role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("worker grant unavailable")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA execution,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations,execution.jobs,execution.run_attempts,execution.runs TO " + id,
		"GRANT SELECT (workspace_id,run_id,deployment_revision) ON execution.run_admissions TO " + id,
		"GRANT UPDATE (state,blocked_reason,available_at,lease_owner,lease_until,lease_generation,attempt_count,updated_at) ON execution.jobs TO " + id,
		"GRANT INSERT ON execution.run_attempts TO " + id,
		"GRANT UPDATE (state,lease_until,finished_at,submission_key,submission_intent_at,provider_id,provider_request_id,external_task_id,submitted_at,unknown_at,unknown_reason) ON execution.run_attempts TO " + id,
		"GRANT UPDATE (state,version,updated_at) ON execution.runs TO " + id,
		"GRANT INSERT ON execution.run_events TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("worker grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("worker grant commit failed")
	}
	return nil
}
