//go:build integration

package integration_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	connfacade "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/inbound/facade"
	connpg "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/postgres"
	connapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	execfacade "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/facade"
	execpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	execapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	connectioncredentials "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/connectioncredentials"
	executioninput "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/executioninput"
	supplypg "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/postgres"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	admissionapp "github.com/orz-i/mender/backend/internal/processes/admission/application"
	"github.com/orz-i/mender/backend/migrations"
)

func expectPlatformPGCode(t *testing.T, ctx context.Context, pool *pgxpool.Pool, code, statement string, args ...any) {
	t.Helper()
	_, err := pool.Exec(ctx, statement, args...)
	if err == nil || pgErrorCode(err) != code {
		t.Fatalf("expected PostgreSQL code %s, got code=%s err=%v", code, pgErrorCode(err), err)
	}
}

func exercisePlatformAdminOperations(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	manager, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_platform_admin_", migrations.GrantPlatformAdminManager)
	browser, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_platform_browser_", migrations.GrantBrowserSession)
	executor, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_platform_executor_", migrations.GrantExecutor)
	must(t, database.PlatformAdminManagerRole(ctx, manager))
	must(t, database.BrowserSessionRole(ctx, browser))
	must(t, database.ExecutorRole(ctx, executor))
	if database.PlatformAdminManagerRole(ctx, owner) == nil || database.PlatformAdminManagerRole(ctx, runtime) == nil || database.PlatformAdminManagerRole(ctx, browser) == nil {
		t.Fatal("elevated/runtime/cross-purpose role accepted as platform-admin-manager")
	}
	for _, table := range []string{"identity.workspaces", "identity.workspace_admin_states", "supply.deployments", "supply.provider_admin_states", "governance.platform_incidents", "governance.platform_admin_audit_events", "execution.runs", "connections.connections", "commerce.budget_periods"} {
		expectPlatformPGCode(t, ctx, manager, "42501", `SELECT count(*) FROM `+table)
	}

	base := time.Now().UTC().Truncate(time.Microsecond).Add(-2 * time.Minute)
	workspace := "ws_admin_t26"
	provider := "provider_admin_t26"
	operator := "staff_admin_operator"
	reviewer := "staff_admin_reviewer"
	auditor := "staff_admin_auditor"
	tenantUser := "user_admin_tenant"
	keyID := strings.Repeat("a", 32)
	_, err := owner.Exec(ctx, `INSERT INTO identity.workspaces(id,created_at) VALUES($1,$2)`, workspace, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.service_accounts(workspace_id,id,created_at) VALUES($1,'sa_admin_t26',$2)`, workspace, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.api_keys(id,workspace_id,subject_id,digest,scopes,created_at,expires_at) VALUES($1,$2,'sa_admin_t26',$3,ARRAY['run:read']::text[],$4,$5)`, keyID, workspace, strings.Repeat("b", 64), base, base.Add(2*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.users(id,display_name,created_at) VALUES
	 ($1,'Tenant User',$5),($2,'Platform Operator',$5),($3,'Platform Reviewer',$5),($4,'Platform Auditor',$5)`, tenantUser, operator, reviewer, auditor, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.workspace_memberships(workspace_id,user_id,role,created_at) VALUES($1,$2,'admin',$3)`, workspace, tenantUser, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.platform_staff(user_id,role,created_at) VALUES
	 ($1,'operator',$4),($2,'reviewer',$4),($3,'auditor',$4)`, operator, reviewer, auditor, base)
	must(t, err)
	var platformMemberships int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM identity.workspace_memberships WHERE workspace_id=$1 AND user_id IN ($2,$3,$4)`, workspace, operator, reviewer, auditor).Scan(&platformMemberships))
	if platformMemberships != 0 {
		t.Fatal("Platform Staff unexpectedly received tenant membership", platformMemberships)
	}

	_, err = owner.Exec(ctx, `INSERT INTO identity.run_delegations(id,digest,workspace_id,user_id,scopes,created_at,expires_at) VALUES
	 ('deleg_admin_t26',$1,$2,$3,ARRAY['run:read']::text[],$4,$5)`, strings.Repeat("c", 64), workspace, tenantUser, base, base.Add(time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.run_start_delegations(
	 id,digest,workspace_id,user_id,toolset_version_id,tool_id,tool_version,tool_version_id,connection_id,currency,max_charge_micro,idempotency_key,arguments_hash,created_at,expires_at)
	 VALUES('start_admin_t26',$1,$2,$3,'set_admin_t26','tool_admin_t26','1.0.0','tv_admin_t26','conn_admin_t26','USD',100,'admin-start-0001',$4,$5,$6)`,
		strings.Repeat("d", 64), workspace, tenantUser, strings.Repeat("e", 64), base, base.Add(time.Hour))
	must(t, err)

	checkAt := base.Add(time.Minute)
	credentialRepo := identitypg.New(runtime)
	credential, err := credentialRepo.FindCredential(ctx, keyID)
	must(t, err)
	if !credential.ActiveAt(checkAt) {
		t.Fatal("active workspace credential unexpectedly inactive before freeze")
	}
	humanRepo := identitypg.NewHumanSessions(browser)
	membership, err := humanRepo.FindWorkspaceMembership(ctx, tenantUser, workspace)
	must(t, err)
	if !membership.Active() {
		t.Fatal("active workspace membership unexpectedly inactive before freeze")
	}
	delegations := identitypg.NewRunDelegations(browser, browser)
	delegation, err := delegations.FindRunDelegationByID(ctx, "deleg_admin_t26")
	must(t, err)
	if !delegation.ActiveAt(checkAt) {
		t.Fatal("Run delegation unexpectedly inactive before freeze")
	}
	startDelegations := identitypg.NewRunStartDelegations(browser, browser)
	startDelegation, err := startDelegations.FindRunStartDelegationByID(ctx, "start_admin_t26")
	must(t, err)
	if !startDelegation.AllowsCreate(checkAt) {
		t.Fatal("StartRun delegation unexpectedly inactive before freeze")
	}

	// Reviewer/auditor identities are visible to the function surface but only
	// PlatformOperator may mutate workspace/provider/incident state.
	expectPlatformPGCode(t, ctx, manager, "42501", `SELECT * FROM governance.platform_admin_set_workspace_frozen($1,1,true,$2,'reviewer cannot freeze',$3)`, workspace, reviewer, checkAt)
	_, err = manager.Exec(ctx, `SELECT * FROM governance.platform_admin_set_workspace_frozen($1,1,true,$2,'security freeze',$3)`, workspace, operator, checkAt)
	must(t, err)
	expectPlatformPGCode(t, ctx, manager, "40001", `SELECT * FROM governance.platform_admin_set_workspace_frozen($1,1,false,$2,'stale unfreeze',$3)`, workspace, operator, checkAt.Add(time.Second))

	credential, err = credentialRepo.FindCredential(ctx, keyID)
	must(t, err)
	membership, err = humanRepo.FindWorkspaceMembership(ctx, tenantUser, workspace)
	must(t, err)
	delegation, err = delegations.FindRunDelegationByID(ctx, "deleg_admin_t26")
	must(t, err)
	startDelegation, err = startDelegations.FindRunStartDelegationByID(ctx, "start_admin_t26")
	must(t, err)
	if !credential.WorkspaceDisabled || credential.ActiveAt(checkAt) || !membership.WorkspaceDisabled || membership.Active() || !delegation.WorkspaceDisabled || delegation.ActiveAt(checkAt) || !startDelegation.WorkspaceDisabled || startDelegation.AllowsCreate(checkAt) {
		t.Fatal("workspace freeze did not immediately revoke tenant-effective authorization")
	}
	_, err = manager.Exec(ctx, `SELECT * FROM governance.platform_admin_set_workspace_frozen($1,2,false,$2,'incident cleared',$3)`, workspace, operator, checkAt.Add(2*time.Second))
	must(t, err)

	// Seed one real provider path. Deployment insertion seeds durable active
	// provider admin state without exposing it to runtime/executor roles.
	_, err = owner.Exec(ctx, `INSERT INTO supply.deployments(
	 revision,provider_id,transport_kind,endpoint_url,http_method,auth_mode,auth_header_name,idempotency_header,
	 request_timeout_ms,max_request_bytes,max_response_bytes,state,created_at)
	 VALUES('deploy_admin_t26',$1,'http','https://admin-provider.example.test/submit','POST','bearer',NULL,'Idempotency-Key',1000,65536,8192,'active',$2)`, provider, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,starts_at,ends_at,active,charge_micro,billing_policy)
	 VALUES('price_admin_t26','tv_admin_t26','USD',100,$1,$2,true,100,'fixed_success_only')`, base, base.Add(2*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO catalog.tool_versions(
	 id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,published_at)
	 VALUES('tv_admin_t26','tool_admin_t26','1.0.0',$1,'price_admin_t26','deploy_admin_t26','Admin Tool','T26 provider fixture',
	 '{"type":"object","additionalProperties":true}'::jsonb,'{"type":"object"}'::jsonb,'read_only','safe_read',false,'published',$2)`, provider, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at)
	 VALUES($1,'conn_admin_t26',$2,'cred_secret_admin_t26','active',1,$3,$4)`, workspace, provider, base, base.Add(2*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at)
	 VALUES($1,'conn_admin_t26','sa_admin_t26',true,$2,$3)`, workspace, base, base.Add(2*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro)
	 VALUES($1,'budget_admin_t26','period_admin_t26','USD',$2,$3,100000)`, workspace, base, base.Add(2*time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO distribution.toolsets(workspace_id,id,state,created_at,updated_at,published_at)
	 VALUES($1,'set_admin_t26','published',$2,$2,$2)`, workspace, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO distribution.toolset_bindings(
	 workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,mcp_name,mcp_exposed,state,published_at)
	 VALUES($1,'set_admin_t26','tool_admin_t26','1.0.0','tv_admin_t26','budget_admin_t26','conn_admin_t26',NULL,false,'published',$2)`, workspace, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO execution.runs(workspace_id,id,state,version,created_at,updated_at) VALUES($1,'run_admin_t26','queued',1,$2,$2)`, workspace, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO execution.run_admissions(
	 workspace_id,run_id,subject_id,credential_id,idempotency_key,request_hash,reservation_id,tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,period_id,currency,reserved_micro,canonical_arguments,created_at)
	 VALUES($1,'run_admin_t26','sa_admin_t26',$2,'admin-provider-0001',repeat('f',64),'reservation_admin_t26','tv_admin_t26','set_admin_t26','conn_admin_t26','price_admin_t26','deploy_admin_t26','budget_admin_t26','period_admin_t26','USD',100,'{"secret":"tenant-payload"}',$3)`, workspace, keyID, base)
	must(t, err)

	resolver, err := bootstrap.BuildAdmissionResolver(runtime)
	must(t, err)
	resolve := func() (admissionapp.Plan, error) {
		return resolver.Resolve(ctx, admissionapp.Caller{WorkspaceID: workspace, SubjectID: "sa_admin_t26", CredentialID: keyID}, admissionapp.Request{
			ToolsetVersionID: "set_admin_t26", ToolID: "tool_admin_t26", ToolVersion: "1.0.0", ConnectionID: "conn_admin_t26", Currency: "USD",
		}, `{}`)
	}
	plan, err := resolve()
	must(t, err)
	if plan.DeploymentRevision != "deploy_admin_t26" {
		t.Fatal("production resolver did not select active provider deployment", plan)
	}
	var providerAllowed bool
	must(t, runtime.QueryRow(ctx, `SELECT supply.provider_accepts_new_work($1)`, provider).Scan(&providerAllowed))
	if !providerAllowed {
		t.Fatal("active provider was blocked before quarantine")
	}
	if _, err = runtime.Exec(ctx, `SELECT count(*) FROM supply.provider_admin_states`); err == nil {
		t.Fatal("runtime role obtained raw provider admin state")
	}
	if _, err = executor.Exec(ctx, `SELECT count(*) FROM supply.provider_admin_states`); err == nil {
		t.Fatal("executor role obtained raw provider admin state")
	}

	execService, err := execapp.NewRuntimeInputService(execpg.NewRuntimeInputs(executor))
	must(t, err)
	connService, err := connapp.NewRuntimeService(connpg.NewRuntimeRepository(executor))
	must(t, err)
	secrets := &integrationSecretProvider{}
	broker, err := supplyapp.NewBroker(
		executioninput.New(execfacade.NewRuntimeInputs(execService)),
		connectioncredentials.New(connfacade.NewRuntimeCredentials(connService)),
		supplypg.NewDeployments(executor), secrets,
	)
	must(t, err)
	_, err = broker.Prepare(ctx, supplyapp.InvocationRef{WorkspaceID: workspace, RunID: "run_admin_t26"}, checkAt)
	must(t, err)
	if secrets.calls != 1 {
		t.Fatal("active provider submission did not resolve its opaque secret once", secrets.calls)
	}

	expectPlatformPGCode(t, ctx, manager, "42501", `SELECT * FROM governance.platform_admin_set_provider_state($1,1,'quarantined',$2,'reviewer cannot quarantine',$3)`, provider, reviewer, checkAt)
	_, err = manager.Exec(ctx, `SELECT * FROM governance.platform_admin_set_provider_state($1,1,'quarantined',$2,'upstream anomaly',$3)`, provider, operator, checkAt)
	must(t, err)
	expectPlatformPGCode(t, ctx, manager, "40001", `SELECT * FROM governance.platform_admin_set_provider_state($1,1,'active',$2,'stale restore',$3)`, provider, operator, checkAt.Add(time.Second))
	must(t, runtime.QueryRow(ctx, `SELECT supply.provider_accepts_new_work($1)`, provider).Scan(&providerAllowed))
	if providerAllowed {
		t.Fatal("quarantined provider still accepted new work")
	}
	if _, err = resolve(); !errors.Is(err, admissionapp.ErrForbidden) {
		t.Fatal("quarantined provider remained admissible", err)
	}
	secretCalls := secrets.calls
	if _, err = broker.Prepare(ctx, supplyapp.InvocationRef{WorkspaceID: workspace, RunID: "run_admin_t26"}, checkAt); !errors.Is(err, supplyapp.ErrInvocationForbidden) || secrets.calls != secretCalls {
		t.Fatal("quarantined provider submission reached credential secret resolution", err, secrets.calls, secretCalls)
	}
	control, err := broker.PrepareControl(ctx, supplyapp.InvocationRef{WorkspaceID: workspace, RunID: "run_admin_t26"}, checkAt)
	must(t, err)
	if control.Deployment.Revision != "deploy_admin_t26" || secrets.calls != secretCalls+1 {
		t.Fatal("quarantine blocked existing-run control/reconciliation material", control.Deployment.Revision, secrets.calls)
	}
	var deploymentState, pinnedRevision string
	must(t, owner.QueryRow(ctx, `SELECT state FROM supply.deployments WHERE revision='deploy_admin_t26'`).Scan(&deploymentState))
	must(t, owner.QueryRow(ctx, `SELECT deployment_revision FROM execution.run_admissions WHERE workspace_id=$1 AND run_id='run_admin_t26'`, workspace).Scan(&pinnedRevision))
	if deploymentState != "active" || pinnedRevision != "deploy_admin_t26" {
		t.Fatal("provider quarantine rewrote immutable deployment/admission facts", deploymentState, pinnedRevision)
	}
	_, err = manager.Exec(ctx, `SELECT * FROM governance.platform_admin_set_provider_state($1,2,'active',$2,'provider recovered',$3)`, provider, operator, checkAt.Add(2*time.Second))
	must(t, err)
	if _, err = resolve(); err != nil {
		t.Fatal("restored provider did not resume new admission", err)
	}
	if _, err = broker.Prepare(ctx, supplyapp.InvocationRef{WorkspaceID: workspace, RunID: "run_admin_t26"}, checkAt); err != nil {
		t.Fatal("restored provider did not resume new submission", err)
	}

	incidentID := "incident_admin_t26"
	expectPlatformPGCode(t, ctx, manager, "22023", `SELECT * FROM governance.platform_admin_open_incident($1,'provider',$2,'critical','provider.timeout',$3,'reviewer cannot open',$4)`, incidentID+"_reviewer", provider, reviewer, checkAt)
	_, err = manager.Exec(ctx, `SELECT * FROM governance.platform_admin_open_incident($1,'provider',$2,'critical','provider.timeout',$3,'observed upstream timeout',$4)`, incidentID, provider, operator, checkAt.Add(3*time.Second))
	must(t, err)
	expectPlatformPGCode(t, ctx, manager, "40001", `SELECT * FROM governance.platform_admin_resolve_incident($1,2,$2,'stale resolution',$3)`, incidentID, operator, checkAt.Add(4*time.Second))
	_, err = manager.Exec(ctx, `SELECT * FROM governance.platform_admin_resolve_incident($1,1,$2,'provider recovered',$3)`, incidentID, operator, checkAt.Add(5*time.Second))
	must(t, err)
	var incidentState string
	var incidentRevision int64
	must(t, manager.QueryRow(ctx, `SELECT state,revision FROM governance.platform_admin_incidents($1,'resolved','provider',$2,NULL,'',100) WHERE id=$3`, auditor, provider, incidentID).Scan(&incidentState, &incidentRevision))
	if incidentState != "resolved" || incidentRevision != 2 {
		t.Fatal("incident resolution projection drifted", incidentState, incidentRevision)
	}

	rows, err := manager.Query(ctx, `SELECT sequence,event_kind,target_kind,target_id,target_revision,actor_user_id,reason,occurred_at FROM governance.platform_admin_audit_export($1,0,100)`, auditor)
	must(t, err)
	defer rows.Close()
	wanted := map[string]bool{"workspace_frozen": false, "workspace_unfrozen": false, "provider_quarantined": false, "provider_restored": false, "incident_opened": false, "incident_resolved": false}
	for rows.Next() {
		var sequence, targetRevision int64
		var eventKind, targetKind, targetID, actorID, reason string
		var occurredAt time.Time
		must(t, rows.Scan(&sequence, &eventKind, &targetKind, &targetID, &targetRevision, &actorID, &reason, &occurredAt))
		if sequence < 1 || targetRevision < 1 || actorID == "" || occurredAt.IsZero() {
			t.Fatal("invalid platform audit projection", sequence, eventKind, targetRevision, actorID, occurredAt)
		}
		if strings.Contains(reason, "tenant-payload") || strings.Contains(reason, "cred_secret_admin_t26") {
			t.Fatal("platform audit export leaked tenant payload or credential reference", reason)
		}
		if targetID == workspace || targetID == provider || targetID == incidentID {
			if _, ok := wanted[eventKind]; ok {
				wanted[eventKind] = true
			}
		}
	}
	must(t, rows.Err())
	for eventKind, seen := range wanted {
		if !seen {
			t.Fatal("platform audit export missing event", eventKind)
		}
	}
	expectPlatformPGCode(t, ctx, owner, "42501", `UPDATE governance.platform_admin_audit_events SET reason='tamper' WHERE target_id=$1`, workspace)
	expectPlatformPGCode(t, ctx, owner, "42501", `DELETE FROM governance.platform_incidents WHERE id=$1`, incidentID)
	if err = manager.QueryRow(ctx, `SELECT count(*) FROM governance.platform_admin_workspaces($1)`, auditor).Scan(new(int)); err != nil {
		t.Fatal("auditor could not read bounded workspace admin projection", err)
	}
	expectPlatformPGCode(t, ctx, manager, "42501", `SELECT * FROM governance.platform_admin_providers($1)`, auditor)

	t.Log("T26 Platform Admin verified: platform/tenant separation, workspace freeze revalidation, provider quarantine new-work gates, existing-run control continuity, incidents, audit export and function-only database authority")
}
