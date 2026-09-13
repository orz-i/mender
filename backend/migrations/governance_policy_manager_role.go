package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantGovernancePolicyManager grants only publication-policy lifecycle authority.
// The role can read policy revisions/decisions and invoke reviewed create/activate
// functions. It cannot mutate Catalog, approvals, audit history, secrets or execution.
func GrantGovernancePolicyManager(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid governance-policy-manager role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("governance-policy-manager role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("governance-policy-manager grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA governance,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT SELECT ON governance.catalog_publication_policy_revisions,governance.catalog_publication_policy_decisions TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.create_catalog_publication_policy(text,text,text,text,boolean,boolean,timestamptz),governance.activate_catalog_publication_policy(text,text,text,timestamptz) TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("governance-policy-manager grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("governance-policy-manager grant commit failed")
	}
	return nil
}
