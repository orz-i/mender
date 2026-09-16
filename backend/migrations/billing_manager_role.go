package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantBillingManager exposes only controlled S4-03 billing functions. The
// role never receives direct ledger, settlement, execution or approval-table
// access; exact approval consumption occurs inside SECURITY DEFINER functions.
func GrantBillingManager(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid billing-manager role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("billing-manager role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("billing-manager grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA commerce,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT EXECUTE ON FUNCTION commerce.billing_summary(text,text,text) TO " + id,
		"GRANT EXECUTE ON FUNCTION commerce.billing_reconciliation(text,text,text) TO " + id,
		"GRANT EXECUTE ON FUNCTION commerce.post_billing_refund(text,text,text,bigint,text,text,text,text,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION commerce.post_billing_adjustment(text,text,text,text,text,bigint,text,text,text,text,timestamptz) TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("billing-manager grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("billing-manager grant commit failed")
	}
	return nil
}
