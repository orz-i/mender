package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantReconciler provisions a result writer only. It can inspect submitted
// execution control facts and append provider observations, but cannot read
// admitted arguments, credentials, identity, commerce, Supply or Outbox data.
func GrantReconciler(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid reconciler role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("reconciler role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("reconciler grant unavailable")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA execution,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations,execution.runs,execution.jobs,execution.run_attempts,execution.provider_observations TO " + id,
		"GRANT INSERT ON execution.provider_observations,execution.run_events TO " + id,
		"GRANT UPDATE (state,version,updated_at) ON execution.runs TO " + id,
		"GRANT UPDATE (state,blocked_reason,updated_at,stopped_at) ON execution.jobs TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("reconciler grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("reconciler grant commit failed")
	}
	return nil
}
