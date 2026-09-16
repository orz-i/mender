package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func GrantSupportReader(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid support-reader role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("support-reader role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("support-reader grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA governance,mender_meta TO " + id,
		"REVOKE USAGE ON SCHEMA execution FROM " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"REVOKE ALL ON execution.runs FROM " + id,
		"REVOKE EXECUTE ON FUNCTION governance.authorize_jit_support(text,text,text,timestamptz) FROM " + id,
		"GRANT EXECUTE ON FUNCTION governance.list_jit_support_runs(text,text,timestamptz) TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("support-reader grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("support-reader grant commit failed")
	}
	return nil
}
