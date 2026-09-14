package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantArtifactObjectReader provisions only object-sidecar metadata reads.
// It cannot read inline Artifact content, provider evidence, identities or money.
func GrantArtifactObjectReader(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid artifact object reader role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("artifact object reader role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("artifact object reader grant unavailable")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA execution,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT SELECT ON execution.artifact_objects TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("artifact object reader grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("artifact object reader grant commit failed")
	}
	return nil
}
