//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	execpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	supplystatus "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/supplystatus"
	execapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	execdomain "github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type integrationUpstreamMCP struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  int
	auth   []string
}

func newIntegrationUpstreamMCP(t *testing.T, platformKey string) *integrationUpstreamMCP {
	t.Helper()
	f := &integrationUpstreamMCP{}
	server := mcp.NewServer(&mcp.Implementation{Name: "integration-upstream", Version: "v0.0.1"}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}})
	server.AddTool(&mcp.Tool{
		Name: "company.search", Title: "Company Search", Description: "Searches\nreviewed upstream data.",
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false,"required":["query"],"properties":{"query":{"type":"string","minLength":1}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"companies":{"type":"array"},"source":{"type":"string"}}}`),
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		f.mu.Lock()
		f.calls++
		f.mu.Unlock()
		if req == nil || req.Params == nil || req.Params.Name != "company.search" {
			t.Fatal("unexpected upstream call", req)
		}
		return &mcp.CallToolResult{StructuredContent: map[string]any{"companies": []any{map[string]any{"name": "Acme"}}, "source": "mcp-e2e"}}, nil
	})
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, PropagateRequestCancellation: true})
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := r.Header.Get("Authorization")
		f.mu.Lock()
		f.auth = append(f.auth, value)
		f.mu.Unlock()
		if value != "Bearer integration-secret-value" || value == "Bearer "+platformKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		transport.ServeHTTP(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func exerciseUpstreamMCPRuntime(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN, createKey string) {
	workerDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_mcp_worker_", migrations.GrantWorker, database.WorkerRole)
	connectorDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_mcp_connector_", migrations.GrantMCPConnector, database.MCPConnectorRole)
	reconcilerDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_mcp_reconcile_", migrations.GrantReconciler, database.ReconcilerRole)
	admissionDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_mcp_admit_", migrations.GrantAdmission, database.AdmissionRole)

	upstream := newIntegrationUpstreamMCP(t, createKey)
	u, err := url.Parse(upstream.server.URL)
	must(t, err)
	at := time.Now().UTC().Truncate(time.Microsecond)

	_, err = owner.Exec(ctx, `INSERT INTO supply.deployments(revision,provider_id,transport_kind,endpoint_url,http_method,auth_mode,auth_header_name,idempotency_header,mcp_protocol_version,mcp_stateless,request_timeout_ms,max_request_bytes,max_response_bytes,state,created_at)
	 VALUES('deploy_upstream_mcp','provider_upstream_mcp','mcp_streamable_http',$1,'POST','bearer',NULL,NULL,'2026-07-28',true,3000,65536,1048576,'active',$2)`, upstream.server.URL, at)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at) VALUES('ws_upstream_mcp','conn_upstream_mcp','provider_upstream_mcp','secret_upstream_mcp','active',1,$1,$2)`, at.Add(-time.Hour), at.Add(time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at) VALUES('ws_upstream_mcp','conn_upstream_mcp','sa_upstream_mcp',true,$1,$2)`, at.Add(-time.Hour), at.Add(45*time.Minute))
	must(t, err)

	secrets := &integrationSecretProvider{}
	mcpRuntime, closeMCP, err := bootstrap.BuildSupplierMCPRuntime(ctx, bootstrap.SupplierMCPRuntimeConfig{
		WorkerDatabaseURL: workerDSN, ConnectorDatabaseURL: connectorDSN,
		AllowedHosts: []string{u.Hostname()}, AllowHTTP: true, AllowLoopback: true,
	}, secrets)
	must(t, err)
	defer closeMCP()

	candidates, err := mcpRuntime.Discover(ctx, supplyapp.MCPDiscoveryRef{WorkspaceID: "ws_upstream_mcp", SubjectID: "sa_upstream_mcp", ConnectionID: "conn_upstream_mcp", DeploymentRevision: "deploy_upstream_mcp"})
	must(t, err)
	if len(candidates) != 1 || candidates[0].ToolName != "company.search" || candidates[0].ContentSHA256 == "" {
		t.Fatal("upstream discovery was not persisted as one candidate", candidates)
	}
	if secrets.last.ConnectionID != "conn_upstream_mcp" || secrets.last.CredentialVersionRef != "secret_upstream_mcp" {
		t.Fatal("MCP discovery did not use the opaque Connection credential authority", secrets.last)
	}
	var snapshots int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM supply.mcp_tool_snapshots WHERE deployment_revision='deploy_upstream_mcp' AND tool_name='company.search'`).Scan(&snapshots))
	if snapshots != 1 {
		t.Fatal("discovery snapshot missing", snapshots)
	}

	_, err = owner.Exec(ctx, `INSERT INTO catalog.tool_versions(id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,published_at)
	 VALUES('tool_upstream_mcp_v1','tool_upstream_mcp','1.0.0','provider_upstream_mcp','price_upstream_mcp_v1','deploy_upstream_mcp','Company Search','Reviewed import','{"type":"object","additionalProperties":false,"required":["query"],"properties":{"query":{"type":"string","minLength":1}}}'::jsonb,'{"type":"object","properties":{"companies":{"type":"array"},"source":{"type":"string"}}}'::jsonb,'read_only','safe_read',false,'published',$1)`, at.Add(-time.Minute))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO supply.mcp_tool_routes(tool_version_id,deployment_revision,upstream_tool_name,snapshot_sha256,state,created_at) VALUES('tool_upstream_mcp_v1','deploy_upstream_mcp','company.search',$1,'active',$2)`, candidates[0].ContentSHA256, at)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,state,published_at) VALUES('ws_upstream_mcp','set_upstream_mcp_v1','tool_upstream_mcp','1.0.0','tool_upstream_mcp_v1','budget_upstream_mcp','conn_upstream_mcp','published',$1)`, at.Add(-time.Minute))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,charge_micro,billing_policy,starts_at,ends_at,active) VALUES('price_upstream_mcp_v1','tool_upstream_mcp_v1','USD',70,70,'fixed_success_only',$1,$2,true)`, at.Add(-time.Hour), at.Add(time.Hour))
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro) VALUES('ws_upstream_mcp','budget_upstream_mcp','period_upstream_mcp','USD',$1,$2,1000)`, at.Add(-time.Hour), at.Add(time.Hour))
	must(t, err)

	api, closeAPI, err := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{RunAPIEnabled: true, StartRunAPIEnabled: true, DatabaseURL: runtimeDSN, AdmissionDatabaseURL: admissionDSN})
	must(t, err)
	defer closeAPI()
	body := `{"tool_ref":{"tool_id":"tool_upstream_mcp","version":"1.0.0"},"toolset_id":"set_upstream_mcp_v1","connection_id":"conn_upstream_mcp","arguments":{"query":"Acme"},"max_charge":{"currency":"USD","amount_micro":"100"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/ws_upstream_mcp/runs", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+createKey)
	req.Header.Set("Idempotency-Key", "upstream_mcp_e2e_0001")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	api.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatal("upstream MCP StartRun failed", w.Code, w.Body.String())
	}
	var accepted struct {
		Data struct {
			RunID string `json:"run_id"`
		} `json:"data"`
	}
	must(t, json.Unmarshal(w.Body.Bytes(), &accepted))
	if accepted.Data.RunID == "" {
		t.Fatal("missing Run ID")
	}
	runID := accepted.Data.RunID

	control, closeWorker, err := bootstrap.BuildWorkerControl(ctx, bootstrap.WorkerConfig{ControlEnabled: true, DatabaseURL: workerDSN, WorkerID: "worker_upstream_mcp", Workspaces: []execdomain.WorkspaceID{"ws_upstream_mcp"}})
	must(t, err)
	defer closeWorker()
	activated, err := control.Activate(ctx, "ws_upstream_mcp", []string{"deploy_upstream_mcp"}, 1)
	must(t, err)
	if activated != 1 {
		t.Fatal("MCP job was not activated", activated)
	}
	lease, err := control.LeaseOne(ctx, "ws_upstream_mcp", "worker_upstream_mcp", 30*time.Second)
	must(t, err)
	dispatcher, err := execapp.NewDispatcher(control, mcpRuntime.Executor())
	must(t, err)
	dispatched, err := dispatcher.Dispatch(ctx, lease)
	if err != nil {
		var evidenceCount int
		must(t, owner.QueryRow(ctx, `SELECT count(*) FROM supply.mcp_call_results WHERE workspace_id='ws_upstream_mcp' AND run_id=$1`, runID).Scan(&evidenceCount))
		var attemptState, providerRequestID string
		must(t, owner.QueryRow(ctx, `SELECT state,provider_request_id FROM execution.run_attempts WHERE workspace_id='ws_upstream_mcp' AND run_id=$1 AND attempt_no=1`, runID).Scan(&attemptState, &providerRequestID))
		upstream.mu.Lock()
		callCount := upstream.calls
		authHeaders := append([]string(nil), upstream.auth...)
		upstream.mu.Unlock()
		t.Fatalf("upstream MCP dispatch failed: %v; tools/call=%d call_results=%d attempt=%s provider_request_id=%q auth=%v", err, callCount, evidenceCount, attemptState, providerRequestID, authHeaders)
	}
	if dispatched.Run.State != execdomain.Running || dispatched.Attempt.State != execdomain.AttemptSubmitted || dispatched.Attempt.ProviderID != "provider_upstream_mcp" {
		t.Fatal("upstream MCP dispatch did not enter provider_waiting bundle", dispatched)
	}
	upstream.mu.Lock()
	callCount := upstream.calls
	authHeaders := append([]string(nil), upstream.auth...)
	upstream.mu.Unlock()
	if callCount != 1 {
		t.Fatal("upstream tools/call was retried", callCount)
	}
	for _, header := range authHeaders {
		if header != "Bearer integration-secret-value" || header == "Bearer "+createKey {
			t.Fatal("platform token reached upstream MCP", authHeaders)
		}
	}

	reconcilerPool, err := database.Open(ctx, reconcilerDSN)
	must(t, err)
	defer reconcilerPool.Close()
	status, err := supplystatus.New(map[string]supply.ProviderStatusReader{"provider_upstream_mcp": mcpRuntime.StatusReader()})
	must(t, err)
	results, err := execapp.NewProviderResults(execpg.NewProviderResults(reconcilerPool))
	must(t, err)
	reconciler, err := execapp.NewProviderReconciler(execpg.NewProviderReconciliation(reconcilerPool), status, results)
	must(t, err)
	record, err := reconciler.ReconcileOne(ctx, "ws_upstream_mcp")
	must(t, err)
	if record.Run.State != execdomain.Succeeded || record.Job.State != execdomain.JobFinished || record.Observation.State != execdomain.ProviderSucceeded {
		t.Fatal("upstream MCP result did not converge", record)
	}
	var artifact, source string
	must(t, owner.QueryRow(ctx, `SELECT content_json::text,source_observation_id FROM execution.artifacts WHERE workspace_id='ws_upstream_mcp' AND run_id=$1`, runID).Scan(&artifact, &source))
	if !strings.Contains(artifact, `"mcp-e2e"`) || strings.Contains(artifact, "integration-secret-value") || !strings.HasPrefix(source, "mcp.") {
		t.Fatal("Artifact does not contain the normalized MCP result", artifact, source)
	}
	var settlementJobs int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.settlement_jobs WHERE workspace_id='ws_upstream_mcp' AND run_id=$1`, runID).Scan(&settlementJobs))
	if settlementJobs != 1 {
		t.Fatal("terminal MCP result missed settlement", settlementJobs)
	}

	connectorPool, err := database.Open(ctx, connectorDSN)
	must(t, err)
	defer connectorPool.Close()
	if _, err = connectorPool.Exec(ctx, `SELECT digest FROM identity.api_keys`); err == nil {
		t.Fatal("MCP connector can read platform machine keys")
	}
	if _, err = connectorPool.Exec(ctx, `UPDATE supply.deployments SET state='disabled' WHERE revision='deploy_upstream_mcp'`); err == nil {
		t.Fatal("MCP connector can mutate reviewed deployment")
	}
	if _, err = connectorPool.Exec(ctx, `SELECT limit_micro FROM commerce.budget_periods`); err == nil {
		t.Fatal("MCP connector can read budgets")
	}
	if _, err = mcpRuntime.StatusReader().QueryStatus(ctx, supply.StatusQuery{WorkspaceID: "ws_b", RunID: runID, AttemptNo: 1, ProviderID: "provider_upstream_mcp", ProviderRequestID: dispatched.Attempt.ProviderRequestID}); err == nil {
		t.Fatal("cross-workspace MCP result escaped RLS")
	}

	beforeSecrets := secrets.calls
	_, err = owner.Exec(ctx, `UPDATE connections.connections SET state='revoked' WHERE workspace_id='ws_upstream_mcp' AND id='conn_upstream_mcp'`)
	must(t, err)
	if _, e := mcpRuntime.Discover(ctx, supplyapp.MCPDiscoveryRef{WorkspaceID: "ws_upstream_mcp", SubjectID: "sa_upstream_mcp", ConnectionID: "conn_upstream_mcp", DeploymentRevision: "deploy_upstream_mcp"}); e == nil {
		t.Fatal("revoked Connection still discovered upstream MCP")
	}
	if secrets.calls != beforeSecrets {
		t.Fatal("revoked Connection reached SecretProvider")
	}
	_, err = owner.Exec(ctx, `UPDATE connections.connections SET state='active' WHERE workspace_id='ws_upstream_mcp' AND id='conn_upstream_mcp'`)
	must(t, err)
	_, err = owner.Exec(ctx, `UPDATE supply.deployments SET state='disabled' WHERE revision='deploy_upstream_mcp'`)
	must(t, err)
	beforeSecrets = secrets.calls
	if _, e := mcpRuntime.Discover(ctx, supplyapp.MCPDiscoveryRef{WorkspaceID: "ws_upstream_mcp", SubjectID: "sa_upstream_mcp", ConnectionID: "conn_upstream_mcp", DeploymentRevision: "deploy_upstream_mcp"}); e == nil {
		t.Fatal("disabled Deployment still discovered upstream MCP")
	}
	if secrets.calls != beforeSecrets {
		t.Fatal("disabled Deployment reached SecretProvider")
	}
}
