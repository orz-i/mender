package bootstrap

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrLocalDemoUnavailable = errors.New("local demo fixture unavailable")

// seedLocalDemo inserts one intentionally local, no-auth HTTP capability. It
// exists only so a developer can evaluate the real Catalog -> StartRun ->
// Worker -> Usage path. Production deployment validation never enables it.
func seedLocalDemo(ctx context.Context, pool *pgxpool.Pool, workspace, userID string, at time.Time) error {
	if pool == nil || workspace != "ws_local" || userID == "" || at.IsZero() {
		return ErrLocalDemoUnavailable
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		return err
	}
	var member bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identity.workspace_memberships WHERE workspace_id=$1 AND user_id=$2)`, workspace, userID).Scan(&member); err != nil || !member {
		return ErrLocalDemoUnavailable
	}
	created := at.UTC().Truncate(time.Microsecond)
	ends := created.AddDate(10, 0, 0)
	if _, err = tx.Exec(ctx, `INSERT INTO supply.deployments(
	 revision,provider_id,transport_kind,endpoint_url,http_method,status_endpoint_url,status_http_method,
	 auth_mode,auth_header_name,idempotency_header,request_timeout_ms,max_request_bytes,max_response_bytes,state,created_at)
	 VALUES('deploy_local_demo','provider_local_demo','http','http://127.0.0.1:19080/submit','POST','http://127.0.0.1:19080/status','POST',
	 'none',NULL,'Idempotency-Key',3000,65536,1048576,'active',$1) ON CONFLICT(revision) DO NOTHING`, created); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at)
	 VALUES($1,'conn_local_demo','provider_local_demo','credv_local_demo','active',1,$2,$3) ON CONFLICT(workspace_id,id) DO NOTHING`, workspace, created, ends); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at)
	 VALUES($1,'conn_local_demo',$2,true,$3,$4) ON CONFLICT(workspace_id,connection_id,subject_id) DO UPDATE SET active=true,expires_at=EXCLUDED.expires_at`, workspace, userID, created, ends); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,charge_micro,billing_policy,starts_at,ends_at,active)
	 VALUES('price_local_demo_v1','tv_local_company_lookup','USD',10000,5000,'fixed_success_only',$1,$2,true) ON CONFLICT(id) DO NOTHING`, created, ends); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro)
	 VALUES($1,'budget_local_demo','period_local_demo','USD',$2,$3,10000000) ON CONFLICT(workspace_id,budget_id,period_id) DO NOTHING`, workspace, created, ends); err != nil {
		return err
	}
	const inputSchema = `{"type":"object","additionalProperties":false,"required":["query"],"properties":{"query":{"type":"string","minLength":1,"maxLength":200}}}`
	const outputSchema = `{"type":"object","additionalProperties":false,"required":["source","query","message"],"properties":{"source":{"type":"string"},"query":{"type":"string"},"message":{"type":"string"}}}`
	if _, err = tx.Exec(ctx, `INSERT INTO catalog.tool_versions(id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,published_at)
	 VALUES('tv_local_company_lookup','company_lookup','1.0.0','provider_local_demo','price_local_demo_v1','deploy_local_demo','Company lookup','Search a deterministic local demo provider. No external network request is made.',$1::jsonb,$2::jsonb,'read_only','safe_read',true,'published',$3)
	 ON CONFLICT(id) DO NOTHING`, inputSchema, outputSchema, created); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO catalog.tool_version_management(workspace_id,tool_version_id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,created_at,updated_at,published_at)
	 VALUES($1,'tv_local_company_lookup','company_lookup','1.0.0','provider_local_demo','price_local_demo_v1','deploy_local_demo','Company lookup','Search a deterministic local demo provider. No external network request is made.',$2::jsonb,$3::jsonb,'read_only','safe_read',true,'published',$4,$4,$4)
	 ON CONFLICT(workspace_id,tool_version_id) DO NOTHING`, workspace, inputSchema, outputSchema, created); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO distribution.toolsets(workspace_id,id,state,created_at,updated_at,published_at)
	 VALUES($1,'set_local_demo_v1','published',$2,$2,$2) ON CONFLICT(workspace_id,id) DO NOTHING`, workspace, created); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,mcp_name,mcp_exposed,state,published_at)
	 VALUES($1,'set_local_demo_v1','company_lookup','1.0.0','tv_local_company_lookup','budget_local_demo','conn_local_demo','company_lookup',true,'published',$2)
	 ON CONFLICT(workspace_id,toolset_version_id,tool_version_id) DO NOTHING`, workspace, created); err != nil {
		return err
	}
	var activePolicies int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM governance.execution_policy_revisions WHERE workspace_id=$1 AND state='active'`, workspace).Scan(&activePolicies); err != nil {
		return err
	}
	if activePolicies == 0 {
		if _, err = tx.Exec(ctx, `SELECT governance.create_execution_policy($1,'execution_policy_local_demo_v1',$2,'low','low',true,60,$3)`, workspace, userID, created); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT governance.activate_execution_policy($1,'execution_policy_local_demo_v1',$2,$3)`, workspace, userID, created); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
