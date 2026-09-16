package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantReleaseManager provisions the Admin release-governance role. It may
// inspect release facts and invoke reviewed transition functions, but it has no
// direct DML on release tables, deployments, Catalog, Distribution or Execution.
func GrantReleaseManager(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid release-manager role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("release-manager role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("release-manager grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA supply,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT SELECT ON supply.release_plans,supply.release_routes,supply.release_audit_events TO " + id,
		"GRANT EXECUTE ON FUNCTION supply.release_plan_issues(text,text) TO " + id,
		"GRANT EXECUTE ON FUNCTION supply.create_release_plan(text,text,text,text,text,text,text,text,text,text,text,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION supply.start_release_canary(text,text,text,text,timestamptz,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION supply.promote_release(text,text,text,text,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION supply.drain_release(text,text,text,text,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION supply.rollback_release(text,text,text,text,timestamptz) TO " + id,
		"GRANT EXECUTE ON FUNCTION supply.emergency_disable_release(text,text,text,text,text,timestamptz) TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("release-manager grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("release-manager grant commit failed")
	}
	return nil
}
