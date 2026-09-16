package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func DangerousOperationManagerRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, grants bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("dangerous-operation-manager database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT
	 has_table_privilege(current_user,'governance.dangerous_operation_approvals','SELECT')
	 AND has_table_privilege(current_user,'governance.dangerous_operation_audit_events','SELECT')
	 AND has_table_privilege(current_user,'governance.jit_support_grants','SELECT')
	 AND NOT has_table_privilege(current_user,'governance.dangerous_operation_approvals','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'governance.dangerous_operation_audit_events','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'governance.jit_support_grants','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_function_privilege(current_user,'governance.submit_dangerous_operation(text,text,text,text,text,text,text,text,text,jsonb,text,bigint,text,text,timestamptz,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.request_release_emergency_approval(text,text,text,text,text,timestamptz,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.request_support_jit_approval(text,text,text,text[],integer,text,timestamptz,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.approve_dangerous_operation(text,text,text,timestamptz,text)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.reject_dangerous_operation(text,text,text,timestamptz,text)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.activate_jit_support(text,text,text,text,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.revoke_jit_support(text,text,text,timestamptz,text)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'governance.consume_release_emergency_approval(text,text,text,text,bigint,timestamptz)','EXECUTE')
	 AND NOT has_table_privilege(current_user,'identity.platform_staff','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'identity.workspace_memberships','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_schema_privilege(current_user,'supply','USAGE')
	 AND NOT has_schema_privilege(current_user,'commerce','USAGE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("dangerous-operation-manager grants do not match the restricted contract")
	}
	var rls int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='governance' AND c.relname IN ('dangerous_operation_approvals','dangerous_operation_audit_events','jit_support_grants') AND c.relrowsecurity AND c.relforcerowsecurity`).Scan(&rls); err != nil || rls != 3 {
		return errors.New("dangerous-operation-manager RLS safeguards missing")
	}
	return nil
}
