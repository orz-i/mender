//go:build integration

package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	admissionapp "github.com/orz-i/mender/backend/internal/processes/admission/application"
	"github.com/orz-i/mender/backend/migrations"
)

func releaseManifest() (string, string) {
	body := `{"apiVersion":"mender.io/plugin/v1alpha1","plugin_id":"example.release","version":"1.0.0","publisher_id":"publisher_release","display_name":"Release Plugin","description":"Reviewed API Tool capability for T23.","capabilities":[{"kind":"api_tool","tool_version_id":"tv_release"}]}`
	sum := sha256.Sum256([]byte(body))
	return body, hex.EncodeToString(sum[:])
}

func execReleaseManager(t *testing.T, ctx context.Context, manager *pgxpool.Pool, workspace, statement string, args ...any) {
	t.Helper()
	tx := beginWorkspaceTx(t, ctx, manager, workspace)
	_, err := tx.Exec(ctx, statement, args...)
	must(t, err)
	must(t, tx.Commit(ctx))
}

func exerciseReleaseGovernance(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	manager, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_release_manager_", migrations.GrantReleaseManager)
	must(t, database.ReleaseManagerRole(ctx, manager))
	if database.ReleaseManagerRole(ctx, owner) == nil || database.ReleaseManagerRole(ctx, runtime) == nil {
		t.Fatal("owner/runtime role accepted as release manager")
	}

	workspace := "ws_release_t23"
	// Production Admission uses the real system clock. Keep the reviewed
	// Connection/Price fixture comfortably active for the duration of this run.
	base := time.Date(2026, 9, 15, 20, 0, 0, 0, time.UTC)
	deployments := []string{"deploy_release_stable", "deploy_release_candidate", "deploy_release_promote", "deploy_release_disable"}
	for _, revision := range deployments {
		_, err := owner.Exec(ctx, `INSERT INTO supply.deployments(
		 revision,provider_id,transport_kind,endpoint_url,http_method,auth_mode,auth_header_name,idempotency_header,
		 request_timeout_ms,max_request_bytes,max_response_bytes,state,created_at)
		 VALUES($1,'provider_release','http','https://release.example.test/submit','POST','none',NULL,'Idempotency-Key',1000,65536,8192,'active',$2)`, revision, base)
		must(t, err)
	}

	_, err := owner.Exec(ctx, `INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,starts_at,ends_at,active,charge_micro,billing_policy)
	 VALUES('price_release','tv_release','USD',100,$1,$2,true,100,'fixed_success_only')`, base.Add(-time.Hour), base.Add(24*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO catalog.tool_versions(
	 id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,published_at)
	 VALUES('tv_release','tool_release','1.0.0','provider_release','price_release','deploy_release_stable','Release Tool','T23 fixture',
	 '{"type":"object","additionalProperties":false}'::jsonb,'{"type":"object"}'::jsonb,'read_only','safe_read',false,'published',$1)`, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO catalog.tool_version_management(
	 workspace_id,tool_version_id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,created_at,updated_at,published_at)
	 VALUES($1,'tv_release','tool_release','1.0.0','provider_release','price_release','deploy_release_stable','Release Tool','T23 fixture',
	 '{"type":"object","additionalProperties":false}'::jsonb,'{"type":"object"}'::jsonb,'read_only','safe_read',false,'published',$2,$2,$2)`, workspace, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at)
	 VALUES($1,'conn_release','provider_release','cred_release','active',1,$2,$3)`, workspace, base, base.Add(24*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at)
	 VALUES($1,'conn_release','sa_release',true,$2,$3)`, workspace, base, base.Add(24*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro)
	 VALUES($1,'budget_release','period_release','USD',$2,$3,1000000)`, workspace, base.Add(-time.Hour), base.Add(24*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO distribution.toolsets(workspace_id,id,state,created_at,updated_at,published_at)
	 VALUES($1,'set_release','published',$2,$2,$2)`, workspace, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO distribution.toolset_bindings(
	 workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,mcp_name,mcp_exposed,state,published_at)
	 VALUES($1,'set_release','tool_release','1.0.0','tv_release','budget_release','conn_release',NULL,false,'published',$2)`, workspace, base)
	must(t, err)

	manifest, digest := releaseManifest()
	_, err = owner.Exec(ctx, `INSERT INTO supply.publishers(workspace_id,id,owner_user_id,display_name,state,created_at,updated_at)
	 VALUES($1,'publisher_release','publisher_owner','Release Publisher','active',$2,$2)`, workspace, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO supply.plugins(workspace_id,id,publisher_id,created_by_user_id,created_at)
	 VALUES($1,'example.release','publisher_release','publisher_owner',$2)`, workspace, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO supply.plugin_versions(
	 workspace_id,plugin_id,version,publisher_id,revision,state,manifest_json,manifest_sha256,created_by_user_id,
	 created_at,updated_at,submitted_at,approved_at,published_at)
	 VALUES($1,'example.release','1.0.0','publisher_release',1,'published',$2::jsonb,$3,'publisher_owner',$4,$4,$4,$4,$4)`, workspace, manifest, digest, base)
	must(t, err)

	// Release manager has function authority but no direct release/deployment DML.
	tx := beginWorkspaceTx(t, ctx, manager, workspace)
	if _, err = tx.Exec(ctx, `INSERT INTO supply.release_routes(workspace_id,toolset_version_id,tool_version_id,stable_deployment_revision,mode,revision,updated_at)
	 VALUES($1,'set_release','tv_release','deploy_release_stable','stable',1,$2)`, workspace, base); err == nil || pgErrorCode(err) != "42501" {
		t.Fatal("release manager obtained direct route write authority", err)
	}
	_ = tx.Rollback(ctx)
	tx = beginWorkspaceTx(t, ctx, manager, workspace)
	if _, err = tx.Exec(ctx, `UPDATE supply.deployments SET state='disabled' WHERE revision='deploy_release_stable'`); err == nil || pgErrorCode(err) != "42501" {
		t.Fatal("release manager obtained direct deployment authority", err)
	}
	_ = tx.Rollback(ctx)
	if _, err = runtime.Exec(ctx, `SELECT count(*) FROM supply.release_routes`); err == nil {
		t.Fatal("runtime role obtained raw release route reads")
	}

	resolver, err := bootstrap.BuildAdmissionResolver(runtime)
	must(t, err)
	resolve := func() admissionapp.Plan {
		plan, resolveErr := resolver.Resolve(ctx, admissionapp.Caller{WorkspaceID: workspace, SubjectID: "sa_release", CredentialID: "key_release"}, admissionapp.Request{
			ToolsetVersionID: "set_release", ToolID: "tool_release", ToolVersion: "1.0.0", ConnectionID: "conn_release", Currency: "USD",
		}, `{}`)
		must(t, resolveErr)
		return plan
	}

	// Plan 1: canary -> pin candidate into a real admission fact -> drain -> rollback.
	execReleaseManager(t, ctx, manager, workspace, `SELECT supply.create_release_plan($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		workspace, "release_t23_1", "example.release", "1.0.0", "set_release", "tv_release", "provider_release", "deploy_release_stable", "deploy_release_candidate", "admin_release", "start T23 canary", base.Add(time.Minute))
	execReleaseManager(t, ctx, manager, workspace, `SELECT supply.start_release_canary($1,$2,$3,$4,$5,$6)`,
		workspace, "release_t23_1", "admin_release", "canary candidate", base.Add(2*time.Minute), base.Add(4*time.Minute))
	canaryPlan := resolve()
	if canaryPlan.DeploymentRevision != "deploy_release_candidate" {
		t.Fatal("production admission resolver did not select canary candidate", canaryPlan)
	}
	_, err = owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES($1,'run_release_pinned','queued',1,$2,$2)`, workspace, base.Add(2*time.Minute))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO execution.run_admissions(
	 workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
	 VALUES($1,'run_release_pinned','sa_release','key_release','release-idem-001',repeat('a',64),'reservation_release','tv_release','set_release','conn_release','price_release',$2,'budget_release','period_release','USD',100,'{}',$3)`, workspace, canaryPlan.DeploymentRevision, base.Add(2*time.Minute))
	must(t, err)
	execReleaseManager(t, ctx, manager, workspace, `SELECT supply.drain_release($1,$2,$3,$4,$5)`, workspace, "release_t23_1", "admin_release", "drain candidate", base.Add(3*time.Minute))
	if got := resolve().DeploymentRevision; got != "deploy_release_stable" {
		t.Fatal("drain did not route new admission back to stable", got)
	}
	execReleaseManager(t, ctx, manager, workspace, `SELECT supply.rollback_release($1,$2,$3,$4,$5)`, workspace, "release_t23_1", "admin_release", "rollback candidate", base.Add(4*time.Minute))
	if got := resolve().DeploymentRevision; got != "deploy_release_stable" {
		t.Fatal("rollback did not preserve stable route", got)
	}
	var pinned string
	must(t, owner.QueryRow(ctx, `SELECT deployment_revision FROM execution.run_admissions WHERE workspace_id=$1 AND run_id='run_release_pinned'`, workspace).Scan(&pinned))
	if pinned != "deploy_release_candidate" {
		t.Fatal("historical Run deployment was rewritten by drain/rollback", pinned)
	}

	// Plan 2: real promotion advances only the mutable route stable snapshot.
	execReleaseManager(t, ctx, manager, workspace, `SELECT supply.create_release_plan($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		workspace, "release_t23_2", "example.release", "1.0.0", "set_release", "tv_release", "provider_release", "deploy_release_stable", "deploy_release_promote", "admin_release", "prepare promotion", base.Add(5*time.Minute))
	execReleaseManager(t, ctx, manager, workspace, `SELECT supply.start_release_canary($1,$2,$3,$4,$5,$6)`, workspace, "release_t23_2", "admin_release", "observe promotion", base.Add(6*time.Minute), base.Add(7*time.Minute))
	execReleaseManager(t, ctx, manager, workspace, `SELECT supply.promote_release($1,$2,$3,$4,$5)`, workspace, "release_t23_2", "admin_release", "promote candidate", base.Add(8*time.Minute))
	if got := resolve().DeploymentRevision; got != "deploy_release_promote" {
		t.Fatal("promotion did not advance new admission stable route", got)
	}
	var immutableDefault string
	must(t, owner.QueryRow(ctx, `SELECT deployment_revision FROM catalog.tool_version_management WHERE workspace_id=$1 AND tool_version_id='tv_release'`, workspace).Scan(&immutableDefault))
	if immutableDefault != "deploy_release_stable" {
		t.Fatal("promotion mutated immutable ToolVersion deployment", immutableDefault)
	}

	// Plan 3: emergency disable blocks new admissions, disables only the candidate,
	// and a later rollback restores the promoted stable without erasing disable history.
	execReleaseManager(t, ctx, manager, workspace, `SELECT supply.create_release_plan($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		workspace, "release_t23_3", "example.release", "1.0.0", "set_release", "tv_release", "provider_release", "deploy_release_promote", "deploy_release_disable", "admin_release", "prepare emergency drill", base.Add(9*time.Minute))
	execReleaseManager(t, ctx, manager, workspace, `SELECT supply.start_release_canary($1,$2,$3,$4,$5,$6)`, workspace, "release_t23_3", "admin_release", "observe emergency drill", base.Add(10*time.Minute), base.Add(11*time.Minute))
	execReleaseManager(t, ctx, manager, workspace, `SELECT supply.emergency_disable_release($1,$2,$3,$4,$5)`, workspace, "release_t23_3", "admin_release", "candidate unhealthy", base.Add(10*time.Minute+30*time.Second))
	if _, resolveErr := resolver.Resolve(ctx, admissionapp.Caller{WorkspaceID: workspace, SubjectID: "sa_release", CredentialID: "key_release"}, admissionapp.Request{ToolsetVersionID: "set_release", ToolID: "tool_release", ToolVersion: "1.0.0", ConnectionID: "conn_release", Currency: "USD"}, `{}`); resolveErr == nil {
		t.Fatal("emergency-disabled release still admitted new traffic")
	}
	var disabledState string
	must(t, owner.QueryRow(ctx, `SELECT state FROM supply.deployments WHERE revision='deploy_release_disable'`).Scan(&disabledState))
	if disabledState != "disabled" {
		t.Fatal("emergency disable did not close candidate deployment", disabledState)
	}
	execReleaseManager(t, ctx, manager, workspace, `SELECT supply.rollback_release($1,$2,$3,$4,$5)`, workspace, "release_t23_3", "admin_release", "recover promoted stable", base.Add(12*time.Minute))
	if got := resolve().DeploymentRevision; got != "deploy_release_promote" {
		t.Fatal("rollback after emergency disable did not restore prior stable", got)
	}
	must(t, owner.QueryRow(ctx, `SELECT state FROM supply.deployments WHERE revision='deploy_release_disable'`).Scan(&disabledState))
	if disabledState != "disabled" {
		t.Fatal("rollback silently re-enabled emergency-disabled candidate", disabledState)
	}

	var canaryEvents, drainEvents, rollbackEvents, promotedEvents, disabledEvents int
	must(t, owner.QueryRow(ctx, `SELECT
	 count(*) FILTER (WHERE event_kind='canary_started'),
	 count(*) FILTER (WHERE event_kind='draining'),
	 count(*) FILTER (WHERE event_kind='rolled_back'),
	 count(*) FILTER (WHERE event_kind='promoted'),
	 count(*) FILTER (WHERE event_kind='emergency_disabled')
	 FROM supply.release_audit_events WHERE workspace_id=$1`, workspace).Scan(&canaryEvents, &drainEvents, &rollbackEvents, &promotedEvents, &disabledEvents))
	if canaryEvents != 3 || drainEvents != 1 || rollbackEvents != 2 || promotedEvents != 1 || disabledEvents != 1 {
		t.Fatal("release audit drill is incomplete", canaryEvents, drainEvents, rollbackEvents, promotedEvents, disabledEvents)
	}

	// FORCE RLS prevents the release manager from seeing another workspace even
	// through its permitted SELECT surface.
	tx = beginWorkspaceTx(t, ctx, manager, "ws_release_other")
	var cross int
	must(t, tx.QueryRow(ctx, `SELECT count(*) FROM supply.release_plans WHERE workspace_id=$1`, workspace).Scan(&cross))
	if cross != 0 {
		t.Fatal("release-manager RLS exposed another Workspace", cross)
	}
	must(t, tx.Commit(ctx))
}
