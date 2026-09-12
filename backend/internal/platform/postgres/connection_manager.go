package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ConnectionManagerRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, grants bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("connection-manager database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'connections.connections','workspace_id','SELECT')
	 AND has_column_privilege(current_user,'connections.connections','id','SELECT')
	 AND has_column_privilege(current_user,'connections.connections','provider_id','SELECT')
	 AND has_column_privilege(current_user,'connections.connections','state','SELECT,UPDATE')
	 AND has_column_privilege(current_user,'connections.connections','revision','SELECT,UPDATE')
	 AND has_column_privilege(current_user,'connections.connections','created_at','SELECT')
	 AND has_column_privilege(current_user,'connections.connections','expires_at','SELECT')
	 AND NOT has_column_privilege(current_user,'connections.connections','credential_version_ref','SELECT')
	 AND NOT has_table_privilege(current_user,'connections.connection_grants','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_schema_privilege(current_user,'identity','USAGE')
	 AND NOT has_schema_privilege(current_user,'execution','USAGE')
	 AND NOT has_schema_privilege(current_user,'commerce','USAGE')
	 AND NOT has_schema_privilege(current_user,'supply','USAGE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("connection-manager database grants do not match the restricted contract")
	}
	return nil
}
