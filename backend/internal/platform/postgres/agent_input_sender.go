package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func AgentInputSenderRole(ctx context.Context, pool *pgxpool.Pool) error {
	var unsafe, ok bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication))`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("agent-input-sender database role is privileged")
	}
	if err := pool.QueryRow(ctx, `SELECT
	 has_table_privilege(current_user,'execution.runs','SELECT')
	 AND has_column_privilege(current_user,'execution.runs','state','UPDATE')
	 AND has_column_privilege(current_user,'execution.runs','version','UPDATE')
	 AND has_column_privilege(current_user,'execution.runs','updated_at','UPDATE')
	 AND has_table_privilege(current_user,'execution.run_attempts','SELECT')
	 AND has_table_privilege(current_user,'execution.agent_input_requests','SELECT')
	 AND has_column_privilege(current_user,'execution.agent_input_requests','state','UPDATE')
	 AND has_column_privilege(current_user,'execution.agent_input_requests','answer_sha256','UPDATE')
	 AND has_column_privilege(current_user,'execution.agent_input_requests','submission_id','UPDATE')
	 AND has_column_privilege(current_user,'execution.agent_input_requests','sending_at','UPDATE')
	 AND has_column_privilege(current_user,'execution.agent_input_requests','submitted_at','UPDATE')
	 AND has_column_privilege(current_user,'execution.agent_input_requests','updated_at','UPDATE')
	 AND has_table_privilege(current_user,'execution.run_events','INSERT')
	 AND NOT has_table_privilege(current_user,'execution.agent_input_requests','INSERT,DELETE,TRUNCATE')
	 AND NOT has_table_privilege(current_user,'execution.run_attempts','INSERT,UPDATE,DELETE,TRUNCATE')
	 AND NOT has_schema_privilege(current_user,'identity','USAGE')
	 AND NOT has_schema_privilege(current_user,'connections','USAGE')
	 AND NOT has_schema_privilege(current_user,'commerce','USAGE')
	 AND NOT has_schema_privilege(current_user,'supply','USAGE')`).Scan(&ok); err != nil || !ok {
		return errors.New("agent-input-sender database grants do not match the restricted contract")
	}
	var admission bool
	if err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'execution.run_admissions','SELECT,INSERT,UPDATE,DELETE,TRUNCATE')`).Scan(&admission); err != nil || admission {
		return errors.New("agent-input-sender can access canonical admission input")
	}
	var rls int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='execution' AND c.relname IN('runs','run_attempts','agent_input_requests') AND c.relrowsecurity AND c.relforcerowsecurity`).Scan(&rls); err != nil || rls != 3 {
		return errors.New("agent-input-sender RLS safeguards missing")
	}
	return nil
}
