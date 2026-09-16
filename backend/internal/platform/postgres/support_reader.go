package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func SupportReaderRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, grants bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("support-reader database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT
	 has_column_privilege(current_user,'execution.runs','workspace_id','SELECT')
	 AND has_column_privilege(current_user,'execution.runs','id','SELECT')
	 AND has_column_privilege(current_user,'execution.runs','state','SELECT')
	 AND has_column_privilege(current_user,'execution.runs','version','SELECT')
	 AND has_column_privilege(current_user,'execution.runs','created_at','SELECT')
	 AND has_column_privilege(current_user,'execution.runs','updated_at','SELECT')
	 AND NOT has_table_privilege(current_user,'execution.runs','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'execution.run_admissions','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'execution.run_events','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'execution.artifacts','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
	 AND has_function_privilege(current_user,'governance.authorize_jit_support(text,text,text,timestamptz)','EXECUTE')
	 AND NOT has_table_privilege(current_user,'governance.jit_support_grants','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_schema_privilege(current_user,'identity','USAGE')
	 AND NOT has_schema_privilege(current_user,'commerce','USAGE')
	 AND NOT has_schema_privilege(current_user,'supply','USAGE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("support-reader grants do not match the restricted contract")
	}
	var rls int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='execution' AND c.relname='runs' AND c.relrowsecurity AND c.relforcerowsecurity`).Scan(&rls); err != nil || rls != 1 {
		return errors.New("support-reader Run RLS safeguard missing")
	}
	return nil
}
