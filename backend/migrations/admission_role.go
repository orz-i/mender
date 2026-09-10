package migrations

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"regexp"
)

// GrantAdmission is deliberately not exposed by the operator CLI or API.
// An explicit future provisioning path must keep this role separate from query/cancel.
func GrantAdmission(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid admission role")
	}
	var elevated bool
	if e := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); e != nil || elevated {
		return errors.New("admission role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, e := pool.Begin(ctx)
	if e != nil {
		return errors.New("admission grant unavailable")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA execution,commerce,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations,commerce.budget_periods,commerce.reservations,execution.run_admissions TO " + id,
		"GRANT UPDATE (reserved_micro,revision) ON commerce.budget_periods TO " + id,
		"GRANT INSERT ON commerce.reservations,execution.runs,execution.run_admissions,execution.jobs,execution.outbox TO " + id,
	} {
		if _, e = tx.Exec(ctx, sql); e != nil {
			return errors.New("admission grant failed")
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return errors.New("admission grant commit failed")
	}
	return nil
}
