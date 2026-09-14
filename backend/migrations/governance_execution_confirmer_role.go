package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantGovernanceExecutionConfirmer grants Browser-session backed Human users
// only the reviewed preview/confirmation functions. It cannot consume a
// confirmation, mutate policies, read confirmations, or access execution data.
func GrantGovernanceExecutionConfirmer(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid governance-execution-confirmer role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("governance-execution-confirmer role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("governance-execution-confirmer grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA governance,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations,governance.execution_policy_decisions TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.evaluate_execution_policy(text,text,text,text,text,text,text,text,timestamptz),governance.create_execution_confirmation(text,text,text,text,text,text,text,text,timestamptz,timestamptz) TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("governance-execution-confirmer grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("governance-execution-confirmer grant commit failed")
	}
	return nil
}
