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
	err := pool.QueryRow(ctx, `SELECT NOT EXISTS(
	 SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
	 WHERE (n.nspname,c.relname) IN (('execution','runs'),('execution','run_admissions'),('execution','run_events'),('execution','artifacts'),('governance','jit_support_grants'))
	   AND (has_table_privilege(current_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,c.oid,'SELECT,INSERT,UPDATE'))
	)`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("support-reader direct table privileges are forbidden")
	}
	err = pool.QueryRow(ctx, `SELECT
	 has_function_privilege(current_user,'governance.list_jit_support_runs(text,text,timestamptz)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'governance.authorize_jit_support(text,text,text,timestamptz)','EXECUTE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("support-reader function grants are invalid")
	}
	err = pool.QueryRow(ctx, `SELECT
	 NOT has_schema_privilege(current_user,'execution','USAGE')
	 AND NOT has_schema_privilege(current_user,'identity','USAGE')
	 AND NOT has_schema_privilege(current_user,'commerce','USAGE')
	 AND NOT has_schema_privilege(current_user,'supply','USAGE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("support-reader schema isolation grants are invalid")
	}
	return nil
}
