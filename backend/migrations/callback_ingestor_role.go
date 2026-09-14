package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantCallbackIngestor provisions a dedicated verified-callback writer.
// It can bind callback metadata to existing execution attempts and converge
// provider results, but cannot read admission arguments, credentials, Supply,
// Identity or Commerce data.
func GrantCallbackIngestor(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid callback-ingestor role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("callback-ingestor role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("callback-ingestor grant unavailable")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA execution,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations,execution.runs,execution.jobs,execution.run_attempts,execution.provider_observations,execution.provider_cancel_intents,execution.artifacts,execution.provider_callback_inbox TO " + id,
		"GRANT INSERT ON execution.provider_callback_inbox,execution.provider_observations,execution.run_events,execution.settlement_jobs,execution.artifacts TO " + id,
		"GRANT UPDATE (disposition,reason_code,processed_at,delivery_count,last_received_at) ON execution.provider_callback_inbox TO " + id,
		"GRANT UPDATE (state,version,updated_at) ON execution.runs TO " + id,
		"GRANT UPDATE (state,blocked_reason,updated_at,stopped_at) ON execution.jobs TO " + id,
		"GRANT UPDATE (state,sending_at,resolved_at,outcome_observation_id,unknown_reason) ON execution.provider_cancel_intents TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("callback-ingestor grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("callback-ingestor grant commit failed")
	}
	return nil
}
