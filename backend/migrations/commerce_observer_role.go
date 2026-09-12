package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantCommerceObserver grants a read-only, RLS-bound projection for Console
// quota observability. It is not a payment, settlement-worker or admission role.
func GrantCommerceObserver(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid commerce-observer role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, "SELECT rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb FROM pg_roles WHERE rolname=$1", role).Scan(&elevated); err != nil || elevated {
		return errors.New("commerce-observer role must exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("commerce-observer grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA commerce,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT SELECT (workspace_id,budget_id,period_id,currency,starts_at,ends_at,active,limit_micro,consumed_micro,reserved_micro,revision) ON commerce.budget_periods TO " + id,
		"GRANT SELECT (workspace_id,run_id,budget_id,period_id,currency,amount_micro,state,created_at,released_at,charged_micro,settled_at) ON commerce.reservations TO " + id,
		"GRANT SELECT (workspace_id,run_id,outcome) ON commerce.usage_settlements TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("commerce-observer grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("commerce-observer grant commit failed")
	}
	return nil
}
