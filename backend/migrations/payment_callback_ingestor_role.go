package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func GrantPaymentCallbackIngestor(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid payment-callback-ingestor role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("payment-callback-ingestor role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("payment-callback-ingestor grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA commerce,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT EXECUTE ON FUNCTION commerce.ingest_sandbox_payment_callback(text,text,text,text,text,timestamptz,timestamptz,text,text,text,text,text,bigint,text,timestamptz) TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("payment-callback-ingestor grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("payment-callback-ingestor grant commit failed")
	}
	return nil
}
