package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ReleaseManagerRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, grants bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("release-manager database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT
	 has_table_privilege(current_user,'supply.release_plans','SELECT')
	 AND has_table_privilege(current_user,'supply.release_routes','SELECT')
	 AND has_table_privilege(current_user,'supply.release_audit_events','SELECT')
	 AND NOT has_table_privilege(current_user,'supply.release_plans','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'supply.release_routes','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'supply.release_audit_events','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND has_function_privilege(current_user,'supply.release_plan_issues(text,text)','EXECUTE')
	 AND has_function_privilege(current_user,'supply.create_release_plan(text,text,text,text,text,text,text,text,text,text,text,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'supply.start_release_canary(text,text,text,text,timestamptz,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'supply.promote_release(text,text,text,text,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'supply.drain_release(text,text,text,text,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'supply.rollback_release(text,text,text,text,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'supply.emergency_disable_release(text,text,text,text,text,timestamptz)','EXECUTE')
	 AND NOT has_table_privilege(current_user,'supply.deployments','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'supply.plugin_versions','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_schema_privilege(current_user,'catalog','USAGE')
	 AND NOT has_schema_privilege(current_user,'distribution','USAGE')
	 AND NOT has_schema_privilege(current_user,'execution','USAGE')
	 AND NOT has_schema_privilege(current_user,'commerce','USAGE')
	 AND NOT has_schema_privilege(current_user,'identity','USAGE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("release-manager database grants do not match the restricted contract")
	}
	var rls int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='supply' AND c.relname IN ('release_plans','release_routes','release_audit_events') AND c.relrowsecurity AND c.relforcerowsecurity`).Scan(&rls); err != nil || rls != 3 {
		return errors.New("release-manager RLS safeguards missing")
	}
	return nil
}
