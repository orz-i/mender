package migrations

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GrantBrowserSession grants only the identity/browser-session surface. It is
// intentionally distinct from the machine Run API role and creates no role.
func GrantBrowserSession(ctx context.Context, pool *pgxpool.Pool, role string) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(role) {
		return errors.New("invalid browser-session role")
	}
	var elevated bool
	if err := pool.QueryRow(ctx, "SELECT rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb FROM pg_roles WHERE rolname=$1", role).Scan(&elevated); err != nil || elevated {
		return errors.New("browser-session role must exist and be unprivileged")
	}
	id := pgx.Identifier{role}.Sanitize()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("browser-session grant connection failed")
	}
	defer rollback(tx)
	for _, sql := range []string{
		"GRANT USAGE ON SCHEMA identity,mender_meta TO " + id,
		"GRANT SELECT ON mender_meta.schema_migrations,identity.workspaces,identity.users,identity.workspace_memberships,identity.oidc_identities,identity.browser_sessions,identity.run_delegations,identity.run_start_delegations TO " + id,
		"GRANT INSERT (digest,user_id,csrf_digest,created_at,expires_at,revoked_at) ON identity.browser_sessions TO " + id,
		"GRANT UPDATE (revoked_at) ON identity.browser_sessions TO " + id,
		"GRANT INSERT (id,digest,workspace_id,user_id,scopes,created_at,expires_at,revoked_at) ON identity.run_delegations TO " + id,
		"GRANT UPDATE (revoked_at) ON identity.run_delegations TO " + id,
		"GRANT INSERT (id,digest,workspace_id,user_id,toolset_version_id,tool_id,tool_version,tool_version_id,connection_id,currency,max_charge_micro,idempotency_key,created_at,expires_at,revoked_at) ON identity.run_start_delegations TO " + id,
		"GRANT UPDATE (revoked_at) ON identity.run_start_delegations TO " + id,
	} {
		if _, err = tx.Exec(ctx, sql); err != nil {
			return errors.New("browser-session grant failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("browser-session grant commit failed")
	}
	return nil
}
