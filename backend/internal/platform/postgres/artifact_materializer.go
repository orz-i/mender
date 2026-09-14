package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ArtifactMaterializerRole(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return errors.New("artifact materializer database unavailable")
	}
	var unsafe bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles r WHERE pg_has_role(current_user,r.oid,'MEMBER') AND (r.rolsuper OR r.rolbypassrls OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication))
	 OR EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','supply','mender_meta') AND (pg_has_role(current_user,n.nspowner,'MEMBER') OR has_schema_privilege(current_user,n.oid,'CREATE')))
	 OR EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN('identity','execution','commerce','catalog','distribution','connections','supply','mender_meta') AND c.relkind IN('r','p') AND pg_has_role(current_user,c.relowner,'MEMBER'))
	 OR has_database_privilege(current_user,current_database(),'CREATE')`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("artifact materializer role is privileged")
	}
	for _, schema := range []string{"identity", "commerce", "catalog", "distribution", "connections", "supply"} {
		if err := pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,$1,'USAGE')`, schema).Scan(&unsafe); err != nil || unsafe {
			return errors.New("artifact materializer can reach unrelated business schema")
		}
	}
	var ok bool
	if err := pool.QueryRow(ctx, `SELECT has_schema_privilege(current_user,'execution','USAGE')
	 AND has_schema_privilege(current_user,'mender_meta','USAGE')
	 AND has_table_privilege(current_user,'mender_meta.schema_migrations','SELECT')
	 AND has_column_privilege(current_user,'execution.artifacts','workspace_id','SELECT')
	 AND has_column_privilege(current_user,'execution.artifacts','run_id','SELECT')
	 AND has_column_privilege(current_user,'execution.artifacts','id','SELECT')
	 AND has_column_privilege(current_user,'execution.artifacts','kind','SELECT')
	 AND has_column_privilege(current_user,'execution.artifacts','media_type','SELECT')
	 AND has_column_privilege(current_user,'execution.artifacts','content_json','SELECT')
	 AND has_column_privilege(current_user,'execution.artifacts','created_at','SELECT')
	 AND has_table_privilege(current_user,'execution.artifact_objects','SELECT,INSERT')
	 AND has_column_privilege(current_user,'execution.artifact_objects','state','UPDATE')
	 AND has_column_privilege(current_user,'execution.artifact_objects','deleted_at','UPDATE')`).Scan(&ok); err != nil || !ok {
		return errors.New("artifact materializer role grants are invalid")
	}
	for _, table := range []string{"execution.runs", "execution.jobs", "execution.run_attempts", "execution.provider_observations", "execution.provider_cancel_intents", "execution.run_admissions", "execution.outbox", "execution.settlement_jobs"} {
		if err := pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,$1,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,$1,'SELECT,INSERT,UPDATE')`, table).Scan(&unsafe); err != nil || unsafe {
			return errors.New("artifact materializer has unrelated execution access")
		}
	}
	if err := pool.QueryRow(ctx, `SELECT has_column_privilege(current_user,'execution.artifacts','source_observation_id','SELECT') OR has_table_privilege(current_user,'execution.artifacts','INSERT,UPDATE,DELETE,TRUNCATE') OR has_any_column_privilege(current_user,'execution.artifacts','INSERT,UPDATE')`).Scan(&unsafe); err != nil || unsafe {
		return errors.New("artifact materializer can inspect or mutate internal Artifact facts")
	}
	var rls int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='execution' AND c.relname IN('artifacts','artifact_objects') AND c.relrowsecurity AND c.relforcerowsecurity`).Scan(&rls); err != nil || rls != 2 {
		return errors.New("artifact materializer RLS safeguards missing")
	}
	return nil
}
