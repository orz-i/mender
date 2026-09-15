package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PublisherManagerRole verifies that the Publisher workbench database role
// remains draft-only and cannot bypass governed publication transitions.
func PublisherManagerRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, grants bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("publisher-manager database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT
	 has_table_privilege(current_user,'supply.publishers','SELECT')
	 AND has_column_privilege(current_user,'supply.publishers','workspace_id','INSERT')
	 AND has_column_privilege(current_user,'supply.publishers','display_name','UPDATE')
	 AND NOT has_column_privilege(current_user,'supply.publishers','state','INSERT,UPDATE')
	 AND has_table_privilege(current_user,'supply.plugins','SELECT')
	 AND has_column_privilege(current_user,'supply.plugins','workspace_id','INSERT')
	 AND NOT has_table_privilege(current_user,'supply.plugins','UPDATE,DELETE,TRUNCATE')
	 AND has_table_privilege(current_user,'supply.plugin_versions','SELECT')
	 AND has_column_privilege(current_user,'supply.plugin_versions','manifest_json','INSERT,UPDATE')
	 AND has_column_privilege(current_user,'supply.plugin_versions','manifest_sha256','INSERT,UPDATE')
	 AND NOT has_column_privilege(current_user,'supply.plugin_versions','state','INSERT,UPDATE')
	 AND has_table_privilege(current_user,'supply.plugin_versions','DELETE')
	 AND has_function_privilege(current_user,'supply.plugin_version_publish_issues(text,text,text)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'supply.mark_plugin_submitted(text,text,text,timestamptz)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'supply.mark_plugin_approved(text,text,text,bigint,timestamptz)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'supply.publish_plugin_version(text,text,text,bigint,timestamptz)','EXECUTE')
	 AND NOT has_table_privilege(current_user,'supply.deployments','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_schema_privilege(current_user,'catalog','USAGE')
	 AND NOT has_schema_privilege(current_user,'distribution','USAGE')
	 AND NOT has_schema_privilege(current_user,'connections','USAGE')
	 AND NOT has_schema_privilege(current_user,'commerce','USAGE')
	 AND NOT has_schema_privilege(current_user,'execution','USAGE')
	 AND NOT has_schema_privilege(current_user,'identity','USAGE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("publisher-manager database grants do not match the restricted contract")
	}
	return nil
}
