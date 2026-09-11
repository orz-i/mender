package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantSettlement grants only the same-database capabilities needed to turn a
// confirmed terminal provider fact into Commerce quota usage. It cannot read
// canonical arguments, identities, Connections or Supply runtime material.
func GrantSettlement(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid settlement role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("settlement role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("settlement grant unavailable")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA execution,commerce,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT SELECT ON execution.settlement_jobs TO " + id,
		"GRANT UPDATE (state,finished_at) ON execution.settlement_jobs TO " + id,
		"GRANT SELECT (workspace_id,run_id,reservation_id,price_version_id,budget_id,period_id,currency,reserved_micro) ON execution.run_admissions TO " + id,
		"GRANT SELECT (workspace_id,run_id,observation_id,state,observed_at) ON execution.provider_observations TO " + id,
		"GRANT SELECT ON commerce.price_versions TO " + id,
		"GRANT SELECT (workspace_id,budget_id,period_id,currency,limit_micro,consumed_micro,reserved_micro,revision) ON commerce.budget_periods TO " + id,
		"GRANT UPDATE (consumed_micro,reserved_micro,revision) ON commerce.budget_periods TO " + id,
		"GRANT SELECT (workspace_id,id,run_id,budget_id,period_id,currency,amount_micro,state,created_at,released_at,charged_micro,settled_at) ON commerce.reservations TO " + id,
		"GRANT UPDATE (state,charged_micro,settled_at) ON commerce.reservations TO " + id,
		"GRANT SELECT,INSERT ON commerce.usage_settlements TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("settlement grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("settlement grant commit failed")
	}
	return nil
}
