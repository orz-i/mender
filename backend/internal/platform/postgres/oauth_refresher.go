package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func OAuthRefresherRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, grants bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("oauth-refresher database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT
	 has_column_privilege(current_user,'connections.connections','workspace_id','SELECT')
	 AND has_column_privilege(current_user,'connections.connections','id','SELECT')
	 AND has_column_privilege(current_user,'connections.connections','provider_id','SELECT')
	 AND has_column_privilege(current_user,'connections.connections','credential_version_ref','SELECT')
	 AND has_column_privilege(current_user,'connections.connections','credential_version_ref','UPDATE')
	 AND has_column_privilege(current_user,'connections.connections','state','SELECT')
	 AND has_column_privilege(current_user,'connections.connections','state','UPDATE')
	 AND has_column_privilege(current_user,'connections.connections','revision','SELECT')
	 AND has_column_privilege(current_user,'connections.connections','revision','UPDATE')
	 AND has_column_privilege(current_user,'connections.connections','expires_at','SELECT')
	 AND has_column_privilege(current_user,'connections.connections','expires_at','UPDATE')
	 AND NOT has_column_privilege(current_user,'connections.connections','workspace_id','INSERT,UPDATE')
	 AND NOT has_column_privilege(current_user,'connections.connections','id','INSERT,UPDATE')
	 AND NOT has_column_privilege(current_user,'connections.connections','provider_id','INSERT,UPDATE')
	 AND has_column_privilege(current_user,'connections.connection_grants','workspace_id','SELECT')
	 AND has_column_privilege(current_user,'connections.connection_grants','connection_id','SELECT')
	 AND has_column_privilege(current_user,'connections.connection_grants','active','SELECT')
	 AND has_column_privilege(current_user,'connections.connection_grants','expires_at','SELECT')
	 AND has_column_privilege(current_user,'connections.connection_grants','expires_at','UPDATE')
	 AND NOT has_column_privilege(current_user,'connections.connection_grants','subject_id','SELECT')
	 AND NOT has_table_privilege(current_user,'connections.connection_grants','INSERT,DELETE,TRUNCATE')
	 AND has_table_privilege(current_user,'connections.oauth_refresh_sessions','SELECT')
	 AND has_column_privilege(current_user,'connections.oauth_refresh_sessions','refresh_credential_ref','UPDATE')
	 AND has_column_privilege(current_user,'connections.oauth_refresh_sessions','refresh_secret_revision','UPDATE')
	 AND has_column_privilege(current_user,'connections.oauth_refresh_sessions','connection_revision','UPDATE')
	 AND has_column_privilege(current_user,'connections.oauth_refresh_sessions','granted_scopes','UPDATE')
	 AND has_column_privilege(current_user,'connections.oauth_refresh_sessions','state','UPDATE')
	 AND has_column_privilege(current_user,'connections.oauth_refresh_sessions','last_error_code','UPDATE')
	 AND has_column_privilege(current_user,'connections.oauth_refresh_sessions','updated_at','UPDATE')
	 AND has_column_privilege(current_user,'connections.oauth_refresh_sessions','last_refreshed_at','UPDATE')
	 AND NOT has_table_privilege(current_user,'connections.oauth_refresh_sessions','INSERT,DELETE,TRUNCATE')
	 AND NOT has_schema_privilege(current_user,'identity','USAGE')
	 AND NOT has_schema_privilege(current_user,'execution','USAGE')
	 AND NOT has_schema_privilege(current_user,'commerce','USAGE')
	 AND NOT has_schema_privilege(current_user,'supply','USAGE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("oauth-refresher database grants do not match the restricted contract")
	}
	var protected bool
	if err = pool.QueryRow(ctx, `SELECT c.relrowsecurity AND c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='connections' AND c.relname='oauth_refresh_sessions'`).Scan(&protected); err != nil || !protected {
		return errors.New("oauth-refresher RLS protection missing")
	}
	return nil
}
