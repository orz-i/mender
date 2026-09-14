package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func GovernanceExecutionConfirmerRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, grants bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("governance-execution-confirmer database role is privileged")
	}
	err := pool.QueryRow(ctx, `SELECT
	 has_table_privilege(current_user,'governance.execution_policy_decisions','SELECT')
	 AND NOT has_table_privilege(current_user,'governance.execution_policy_decisions','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'governance.execution_policy_revisions','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'governance.execution_confirmations','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')
	 AND has_function_privilege(current_user,'governance.evaluate_execution_policy(text,text,text,text,text,text,text,text,timestamptz)','EXECUTE')
	 AND has_function_privilege(current_user,'governance.create_execution_confirmation(text,text,text,text,text,text,text,text,timestamptz,timestamptz)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'governance.consume_execution_confirmation(text,text,text,text,text,text,text,timestamptz)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'governance.create_execution_policy(text,text,text,text,boolean,timestamptz)','EXECUTE')
	 AND NOT has_function_privilege(current_user,'governance.activate_execution_policy(text,text,text,timestamptz)','EXECUTE')
	 AND NOT has_schema_privilege(current_user,'execution','USAGE')
	 AND NOT has_schema_privilege(current_user,'commerce','USAGE')
	 AND NOT has_schema_privilege(current_user,'connections','USAGE')
	 AND NOT has_schema_privilege(current_user,'identity','USAGE')`).Scan(&grants)
	if err != nil || !grants {
		return errors.New("governance-execution-confirmer database grants do not match the restricted contract")
	}
	return nil
}
