package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func GovernanceReviewerRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, grants bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("governance-reviewer database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT
	 has_table_privilege(current_user,'governance.catalog_publication_approvals','SELECT')
	 AND NOT has_table_privilege(current_user,'governance.catalog_publication_approvals','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND has_table_privilege(current_user,'governance.catalog_publication_audit_events','SELECT')
	 AND NOT has_table_privilege(current_user,'governance.catalog_publication_audit_events','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND has_table_privilege(current_user,'governance.catalog_publication_policy_revisions','SELECT')
	 AND NOT has_table_privilege(current_user,'governance.catalog_publication_policy_revisions','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND has_table_privilege(current_user,'governance.catalog_publication_policy_decisions','SELECT')
	 AND NOT has_table_privilege(current_user,'governance.catalog_publication_policy_decisions','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND has_table_privilege(current_user,'governance.plugin_publication_approvals','SELECT')
	 AND NOT has_table_privilege(current_user,'governance.plugin_publication_approvals','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND has_table_privilege(current_user,'governance.plugin_publication_audit_events','SELECT')
	 AND NOT has_table_privilege(current_user,'governance.plugin_publication_audit_events','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND has_function_privilege(current_user,'governance.approve_catalog_publication(text,text,text,timestamptz,text)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.reject_catalog_publication(text,text,text,timestamptz,text)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.approve_plugin_publication(text,text,text,timestamptz,text)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.reject_plugin_publication(text,text,text,timestamptz,text)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'governance.submit_plugin_publication(text,text,text,text,text,timestamptz,timestamptz)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'governance.publish_approved_plugin(text,text,text,text,timestamptz)','EXECUTE')
	 AND NOT has_schema_privilege(current_user,'catalog','USAGE')
	 AND NOT has_schema_privilege(current_user,'distribution','USAGE')
	 AND NOT has_schema_privilege(current_user,'connections','USAGE')
	 AND NOT has_schema_privilege(current_user,'commerce','USAGE')
	 AND NOT has_schema_privilege(current_user,'execution','USAGE')
	 AND NOT has_schema_privilege(current_user,'identity','USAGE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("governance-reviewer database grants do not match the restricted contract")
	}
	return nil
}
