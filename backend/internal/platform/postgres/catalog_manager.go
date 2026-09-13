package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CatalogManagerRole verifies the least-privilege DB contract for Human
// Catalog/Toolset management. It is deliberately separate from Browser Session,
// admission, commerce settlement and connection-secret roles.
func CatalogManagerRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, grants bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("catalog-manager database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT
	 has_table_privilege(current_user,'catalog.tool_version_management','SELECT')
	 AND has_column_privilege(current_user,'catalog.tool_version_management','workspace_id','INSERT')
	 AND has_column_privilege(current_user,'catalog.tool_version_management','title','UPDATE')
	 AND NOT has_column_privilege(current_user,'catalog.tool_version_management','state','INSERT,UPDATE')
	 AND has_table_privilege(current_user,'distribution.toolsets','SELECT')
	 AND has_column_privilege(current_user,'distribution.toolsets','workspace_id','INSERT')
	 AND NOT has_column_privilege(current_user,'distribution.toolsets','state','INSERT,UPDATE')
	 AND has_table_privilege(current_user,'distribution.toolset_bindings','SELECT')
	 AND has_column_privilege(current_user,'distribution.toolset_bindings','workspace_id','INSERT')
	 AND has_column_privilege(current_user,'distribution.toolset_bindings','budget_id','UPDATE')
	 AND NOT has_column_privilege(current_user,'distribution.toolset_bindings','state','INSERT,UPDATE')
	 AND has_column_privilege(current_user,'connections.connections','provider_id','SELECT')
	 AND NOT has_column_privilege(current_user,'connections.connections','credential_version_ref','SELECT')
	 AND has_column_privilege(current_user,'commerce.price_versions','reserve_micro','SELECT')
	 AND has_column_privilege(current_user,'commerce.budget_periods','budget_id','SELECT')
	 AND NOT has_column_privilege(current_user,'commerce.budget_periods','limit_micro','SELECT')
	 AND NOT has_table_privilege(current_user,'catalog.tool_versions','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_schema_privilege(current_user,'identity','USAGE')
	 AND NOT has_schema_privilege(current_user,'execution','USAGE')
	 AND NOT has_table_privilege(current_user,'commerce.reservations','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("catalog-manager database grants do not match the restricted contract")
	}
	return nil
}
