package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BrowserSessionRole verifies the dedicated human-console identity principal.
func BrowserSessionRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, grants bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("browser-session database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'identity.users','SELECT')
	 AND has_table_privilege(current_user,'identity.workspace_memberships','SELECT')
	 AND has_table_privilege(current_user,'identity.oidc_identities','SELECT')
	 AND has_table_privilege(current_user,'identity.browser_sessions','SELECT')
	 AND has_column_privilege(current_user,'identity.browser_sessions','digest','INSERT')
	 AND has_column_privilege(current_user,'identity.browser_sessions','revoked_at','UPDATE')
	 AND NOT has_table_privilege(current_user,'identity.users','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'identity.workspace_memberships','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'identity.oidc_identities','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'identity.api_keys','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_schema_privilege(current_user,'execution','USAGE')
	 AND NOT has_schema_privilege(current_user,'commerce','USAGE')
	 AND NOT has_schema_privilege(current_user,'connections','USAGE')
	 AND NOT has_schema_privilege(current_user,'supply','USAGE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("browser-session database grants do not match the restricted contract")
	}
	return nil
}
