package migrations

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"regexp"
)

// GrantCancellation never creates users/passwords or grants create/admit permissions.
func GrantCancellation(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid cancellation role")
	}
	var unsafe bool
	if e := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&unsafe); e != nil || unsafe {
		return errors.New("cancellation role must exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, e := pool.Begin(ctx)
	if e != nil {
		return errors.New("cancellation grant unavailable")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA execution,commerce,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations,commerce.budget_periods,commerce.reservations,execution.runs,execution.jobs,execution.outbox,execution.run_events,execution.run_cancellations TO " + id,
		"GRANT SELECT(workspace_id,run_id,reservation_id,budget_id,period_id,currency,reserved_micro) ON execution.run_admissions TO " + id,
		"GRANT UPDATE(reserved_micro,revision) ON commerce.budget_periods TO " + id,
		"GRANT UPDATE(state,released_at) ON commerce.reservations TO " + id,
		"GRANT UPDATE(state,version,updated_at) ON execution.runs TO " + id,
		"GRANT UPDATE(state,stopped_at) ON execution.jobs TO " + id,
		"GRANT UPDATE(delivery_state) ON execution.outbox TO " + id,
		"GRANT INSERT ON execution.run_cancellations,execution.run_events,execution.outbox TO " + id,
	} {
		if _, e = tx.Exec(ctx, sql); e != nil {
			return errors.New("cancellation grant failed")
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return errors.New("cancellation grant commit failed")
	}
	return nil
}
