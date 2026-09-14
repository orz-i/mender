package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantCallbackObserver provisions the Admin callback Inbox read model only.
// It deliberately receives column-level SELECT grants so raw callback hashes,
// key identities and provider routing handles remain unreachable.
func GrantCallbackObserver(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid callback-observer role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("callback-observer role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("callback-observer grant unavailable")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA execution,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT SELECT (receipt_id,workspace_id,provider_id,event_id,received_at,last_received_at,delivery_count,run_id,observation_id,observation_state,observed_at,disposition,reason_code,processed_at) ON execution.provider_callback_inbox TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("callback-observer grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("callback-observer grant commit failed")
	}
	return nil
}
