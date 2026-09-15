package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantGovernanceReviewer grants maker/checker review authority only. The role
// can inspect Workspace-scoped approval records and invoke reviewed decisions;
// it cannot mutate Catalog, Distribution, Connection, Commerce or Execution.
func GrantGovernanceReviewer(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid governance-reviewer role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreatedb OR rolcreaterole OR rolreplication FROM pg_roles WHERE rolname=$1`, role).Scan(&elevated); err != nil || elevated {
		return errors.New("governance-reviewer role must already exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("governance-reviewer grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA governance,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations TO " + id,
		"GRANT SELECT ON governance.catalog_publication_approvals,governance.catalog_publication_audit_events,governance.catalog_publication_policy_revisions,governance.catalog_publication_policy_decisions,governance.plugin_publication_approvals,governance.plugin_publication_audit_events TO " + id,
		"GRANT EXECUTE ON FUNCTION governance.approve_catalog_publication(text,text,text,timestamptz,text),governance.reject_catalog_publication(text,text,text,timestamptz,text),governance.approve_plugin_publication(text,text,text,timestamptz,text),governance.reject_plugin_publication(text,text,text,timestamptz,text) TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("governance-reviewer grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("governance-reviewer grant commit failed")
	}
	return nil
}
