package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func PlatformAdminManagerRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, ok bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("platform-admin-manager database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT
	 has_schema_privilege(current_user,'governance','USAGE')
	 AND has_table_privilege(current_user,'mender_meta.schema_migrations','SELECT')
	 AND has_function_privilege(current_user,'governance.platform_admin_workspaces(text)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.platform_admin_set_workspace_frozen(text,bigint,boolean,text,text,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.platform_admin_providers(text)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.platform_admin_set_provider_state(text,bigint,text,text,text,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.platform_admin_open_incident(text,text,text,text,text,text,text,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.platform_admin_resolve_incident(text,bigint,text,text,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.platform_admin_incidents(text,text,text,text,timestamptz,text,integer)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.platform_admin_audit_export(text,bigint,integer)','EXECUTE')`).Scan(&ok)
	if err != nil || !ok {
		return errors.New("platform-admin-manager function grants are invalid")
	}
	for _, table := range []string{
		"identity.workspaces", "identity.workspace_admin_states", "identity.workspace_memberships", "identity.api_keys", "identity.platform_staff",
		"supply.deployments", "supply.provider_admin_states", "governance.platform_incidents", "governance.platform_admin_audit_events",
		"governance.dangerous_operation_approvals", "governance.jit_support_grants", "execution.runs", "execution.run_admissions", "connections.connections", "commerce.budget_periods",
	} {
		if err = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$1,'SELECT,INSERT,UPDATE')`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("platform-admin-manager has direct table authority: " + table)
		}
	}
	err = pool.QueryRow(ctx, `SELECT
	 NOT has_schema_privilege(current_user,'identity','USAGE')
	 AND NOT has_schema_privilege(current_user,'supply','USAGE')
	 AND NOT has_schema_privilege(current_user,'execution','USAGE')
	 AND NOT has_schema_privilege(current_user,'connections','USAGE')
	 AND NOT has_schema_privilege(current_user,'commerce','USAGE')
	 AND NOT has_function_privilege(current_user,'governance.request_support_jit_approval(text,text,text,text[],integer,text,timestamptz,timestamptz)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'governance.request_release_emergency_approval(text,text,text,text,text,timestamptz,timestamptz)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'governance.approve_dangerous_operation(text,text,text,timestamptz,text)','EXECUTE')`).Scan(&ok)
	if err != nil || !ok {
		return errors.New("platform-admin-manager isolation grants are invalid")
	}
	return nil
}
