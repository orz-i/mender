//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	supplystatus "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/supplystatus"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	rundomain "github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	supplydomain "github.com/orz-i/mender/backend/internal/contexts/supply/domain"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type g3EntrySpec struct {
	name, toolID, toolVersionID, providerID, deploymentID, connectionID, priceID string
	arguments                                                                    string
}

func g3HTTPAgentServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer integration-secret-value" {
			t.Errorf("G3 provider request did not use reviewed supplier credential: %q", r.Header.Get("Authorization"))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "ws_g3_matrix") || strings.Contains(string(body), "integration-secret-value") {
			t.Errorf("G3 provider request leaked local workspace/secret facts: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/http/submit":
			if !strings.HasPrefix(r.Header.Get("Idempotency-Key"), "mender.submit.") || !strings.Contains(string(body), `"query":"matrix-http"`) {
				t.Errorf("unexpected reviewed HTTP submission: key=%q body=%s", r.Header.Get("Idempotency-Key"), body)
			}
			_, _ = io.WriteString(w, `{"provider_request_id":"http/request-g3","external_task_id":"http/task-g3"}`)
		case "/http/status":
			if !strings.Contains(string(body), `"provider_request_id":"http/request-g3"`) || r.Header.Get("Idempotency-Key") != "" {
				t.Errorf("unexpected reviewed HTTP status query: key=%q body=%s", r.Header.Get("Idempotency-Key"), body)
			}
			observed := time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
			_, _ = io.WriteString(w, `{"observation_id":"obs.g3.http","state":"succeeded","result":{"source":"http","answer":42},"observed_at":"`+observed+`"}`)
		case "/agent/submit":
			if !strings.HasPrefix(r.Header.Get("Idempotency-Key"), "mender.submit.") || !strings.Contains(string(body), `"prompt":"matrix-agent"`) {
				t.Errorf("unexpected reviewed Agent submission: key=%q body=%s", r.Header.Get("Idempotency-Key"), body)
			}
			_, _ = io.WriteString(w, `{"provider_request_id":"agent/request-g3","external_task_id":"agent/task-g3"}`)
		case "/agent/status":
			if !strings.Contains(string(body), `"provider_request_id":"agent/request-g3"`) || r.Header.Get("Idempotency-Key") != "" {
				t.Errorf("unexpected reviewed Agent status query: key=%q body=%s", r.Header.Get("Idempotency-Key"), body)
			}
			observed := time.Now().UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
			_, _ = io.WriteString(w, `{"observation_id":"obs.g3.agent","state":"succeeded","result":{"source":"agent","answer":42},"observed_at":"`+observed+`"}`)
		case "/http/cancel", "/agent/cancel":
			t.Errorf("G3 success matrix unexpectedly issued provider cancellation: %s", r.URL.Path)
			http.Error(w, "unexpected cancellation", http.StatusConflict)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	u, err := url.Parse(server.URL)
	must(t, err)
	return server, u.Hostname()
}

func exerciseG3EntryMatrix(t *testing.T, ctx context.Context, owner *pgxpool.Pool, runtimeDSN, createKey, otherWorkspaceKey string) {
	t.Helper()
	const workspace, subject, toolset = "ws_g3_matrix", "sa_g3_matrix", "set_g3_matrix_v1"
	workerDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_g3_worker_", migrations.GrantWorker, database.WorkerRole)
	executorDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_g3_executor_", migrations.GrantExecutor, database.ExecutorRole)
	connectorDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_g3_connector_", migrations.GrantMCPConnector, database.MCPConnectorRole)
	reconcilerDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_g3_reconciler_", migrations.GrantReconciler, database.ReconcilerRole)
	admissionDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_g3_admission_", migrations.GrantAdmission, database.AdmissionRole)
	settlementDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_g3_settlement_", migrations.GrantSettlement, database.SettlementRole)

	httpAgent, httpAgentHost := g3HTTPAgentServer(t)
	upstream := newIntegrationUpstreamMCP(t, createKey)
	upstreamURL, err := url.Parse(upstream.server.URL)
	must(t, err)
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)

	entries := []g3EntrySpec{
		{name: "http", toolID: "tool_g3_http", toolVersionID: "tool_g3_http_v1", providerID: "provider_g3_http", deploymentID: "deploy_g3_http", connectionID: "conn_g3_http", priceID: "price_g3_http_v1", arguments: `{"query":"matrix-http"}`},
		{name: "mcp", toolID: "tool_g3_mcp", toolVersionID: "tool_g3_mcp_v1", providerID: "provider_g3_mcp", deploymentID: "deploy_g3_mcp", connectionID: "conn_g3_mcp", priceID: "price_g3_mcp_v1", arguments: `{"query":"Acme"}`},
		{name: "agent", toolID: "tool_g3_agent", toolVersionID: "tool_g3_agent_v1", providerID: "provider_g3_agent", deploymentID: "deploy_g3_agent", connectionID: "conn_g3_agent", priceID: "price_g3_agent_v1", arguments: `{"prompt":"matrix-agent"}`},
	}

	_, err = owner.Exec(ctx, `INSERT INTO supply.deployments(revision,provider_id,transport_kind,endpoint_url,http_method,status_endpoint_url,status_http_method,cancel_endpoint_url,cancel_http_method,auth_mode,auth_header_name,idempotency_header,request_timeout_ms,max_request_bytes,max_response_bytes,state,created_at)
	 VALUES('deploy_g3_http','provider_g3_http','http',$1,'POST',$2,'POST',$3,'POST','bearer',NULL,'Idempotency-Key',1500,65536,1048576,'active',$4)`, httpAgent.URL+"/http/submit", httpAgent.URL+"/http/status", httpAgent.URL+"/http/cancel", base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO supply.deployments(revision,provider_id,transport_kind,endpoint_url,http_method,auth_mode,auth_header_name,idempotency_header,mcp_protocol_version,mcp_stateless,request_timeout_ms,max_request_bytes,max_response_bytes,state,created_at)
	 VALUES('deploy_g3_mcp','provider_g3_mcp','mcp_streamable_http',$1,'POST','bearer',NULL,NULL,'2026-07-28',true,3000,65536,1048576,'active',$2)`, upstream.server.URL, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO supply.deployments(revision,provider_id,transport_kind,endpoint_url,http_method,status_endpoint_url,status_http_method,cancel_endpoint_url,cancel_http_method,auth_mode,auth_header_name,idempotency_header,request_timeout_ms,max_request_bytes,max_response_bytes,state,created_at)
	 VALUES('deploy_g3_agent','provider_g3_agent','agent_http',$1,'POST',$2,'POST',$3,'POST','bearer',NULL,'Idempotency-Key',1500,65536,1048576,'active',$4)`, httpAgent.URL+"/agent/submit", httpAgent.URL+"/agent/status", httpAgent.URL+"/agent/cancel", base)
	must(t, err)
	for _, entry := range entries {
		_, err = owner.Exec(ctx, `INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at) VALUES($1,$2,$3,$4,'active',1,$5,$6)`, workspace, entry.connectionID, entry.providerID, "credv_"+entry.name, base, base.Add(time.Hour))
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at) VALUES($1,$2,$3,true,$4,$5)`, workspace, entry.connectionID, subject, base, base.Add(time.Hour))
		must(t, err)
	}

	secrets := &integrationSecretProvider{}
	httpExecutor, closeHTTPExecutor, err := bootstrap.BuildSupplierHTTPExecutor(ctx, bootstrap.SupplierHTTPRuntimeConfig{
		WorkerDatabaseURL: workerDSN, ExecutorDatabaseURL: executorDSN, AllowedHosts: []string{httpAgentHost},
		TransportKinds: []string{supplydomain.TransportHTTP, supplydomain.TransportAgentHTTP}, AllowHTTP: true, AllowLoopback: true,
	}, secrets)
	must(t, err)
	defer closeHTTPExecutor()
	mcpRuntime, closeMCP, err := bootstrap.BuildSupplierMCPRuntime(ctx, bootstrap.SupplierMCPRuntimeConfig{
		WorkerDatabaseURL: workerDSN, ConnectorDatabaseURL: connectorDSN, AllowedHosts: []string{upstreamURL.Hostname()}, AllowHTTP: true, AllowLoopback: true,
	}, secrets)
	must(t, err)
	defer closeMCP()
	candidates, err := mcpRuntime.Discover(ctx, supplyapp.MCPDiscoveryRef{WorkspaceID: workspace, SubjectID: subject, ConnectionID: "conn_g3_mcp", DeploymentRevision: "deploy_g3_mcp"})
	must(t, err)
	if len(candidates) != 1 || candidates[0].ToolName != "company.search" {
		t.Fatal("G3 upstream MCP discovery matrix drift", candidates)
	}

	for _, entry := range entries {
		inputSchema := `{"type":"object","additionalProperties":false,"required":["query"],"properties":{"query":{"type":"string","minLength":1}}}`
		if entry.name == "agent" {
			inputSchema = `{"type":"object","additionalProperties":false,"required":["prompt"],"properties":{"prompt":{"type":"string","minLength":1}}}`
		}
		_, err = owner.Exec(ctx, `INSERT INTO catalog.tool_versions(id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,published_at)
		 VALUES($1,$2,'1.0.0',$3,$4,$5,$6,'G3 certified matrix fixture',$7::jsonb,'{"type":"object","properties":{"source":{"type":"string"},"answer":{"type":"integer"}}}'::jsonb,'read_only','safe_read',false,'published',$8)`, entry.toolVersionID, entry.toolID, entry.providerID, entry.priceID, entry.deploymentID, "G3 "+strings.ToUpper(entry.name), inputSchema, base)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,state,published_at) VALUES($1,$2,$3,'1.0.0',$4,'budget_g3_matrix',$5,'published',$6)`, workspace, toolset, entry.toolID, entry.toolVersionID, entry.connectionID, base)
		must(t, err)
		_, err = owner.Exec(ctx, `INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,charge_micro,billing_policy,starts_at,ends_at,active) VALUES($1,$2,'USD',10,10,'fixed_success_only',$3,$4,true)`, entry.priceID, entry.toolVersionID, base.Add(-time.Hour), base.Add(time.Hour))
		must(t, err)
	}
	_, err = owner.Exec(ctx, `INSERT INTO supply.mcp_tool_routes(tool_version_id,deployment_revision,upstream_tool_name,snapshot_sha256,state,created_at) VALUES('tool_g3_mcp_v1','deploy_g3_mcp','company.search',$1,'active',$2)`, candidates[0].ContentSHA256, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro) VALUES($1,'budget_g3_matrix','period_g3_matrix','USD',$2,$3,1000)`, workspace, base.Add(-time.Hour), base.Add(time.Hour))
	must(t, err)

	api, closeAPI, err := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{
		RunAPIEnabled: true, RunReadAPIEnabled: true, StartRunAPIEnabled: true, DatabaseURL: runtimeDSN, AdmissionDatabaseURL: admissionDSN,
		CursorSigningKey: bytes.Repeat([]byte{31}, 32),
	})
	must(t, err)
	defer closeAPI()
	start := func(entry g3EntrySpec) string {
		t.Helper()
		body := fmt.Sprintf(`{"tool_ref":{"tool_id":"%s","version":"1.0.0"},"toolset_id":"%s","connection_id":"%s","arguments":%s,"max_charge":{"currency":"USD","amount_micro":"20"}}`, entry.toolID, toolset, entry.connectionID, entry.arguments)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/"+workspace+"/runs", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+createKey)
		r.Header.Set("Idempotency-Key", "g3_matrix_"+entry.name+"_0001")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != http.StatusAccepted {
			t.Fatal("G3 StartRun matrix entry failed", entry.name, w.Code, w.Body.String())
		}
		var response struct {
			Data struct {
				RunID string `json:"run_id"`
			} `json:"data"`
		}
		must(t, json.Unmarshal(w.Body.Bytes(), &response))
		if response.Data.RunID == "" {
			t.Fatal("G3 StartRun matrix entry returned no Run ID", entry.name)
		}
		return response.Data.RunID
	}
	runs := map[string]string{}
	for _, entry := range entries {
		runs[entry.name] = start(entry)
	}

	rows, err := owner.Query(ctx, `SELECT tool_version_id,count(*) FROM governance.execution_policy_decisions
	 WHERE workspace_id=$1 AND subject_kind='machine' AND subject_id=$2 AND outcome='allow'
	 GROUP BY tool_version_id ORDER BY tool_version_id`, workspace, subject)
	must(t, err)
	defer rows.Close()
	decisionCounts := map[string]int{}
	for rows.Next() {
		var toolVersionID string
		var count int
		must(t, rows.Scan(&toolVersionID, &count))
		decisionCounts[toolVersionID] = count
	}
	must(t, rows.Err())
	if len(decisionCounts) != len(entries) {
		t.Fatal("G3 adapters did not share the same governance decision path", decisionCounts)
	}
	for _, entry := range entries {
		// Admission evaluates once before opening the UoW and enforces the same
		// server-owned policy again inside the transaction. Both facts must exist
		// for every adapter; the matrix intentionally compares the phase set rather
		// than assuming one audit row per Run.
		if decisionCounts[entry.toolVersionID] != 2 {
			t.Fatal("G3 governance phase count drifted by adapter", entry.name, decisionCounts)
		}
	}

	control, closeWorker, err := bootstrap.BuildWorkerControl(ctx, bootstrap.WorkerConfig{ControlEnabled: true, DatabaseURL: workerDSN, WorkerID: "worker_g3_matrix", Workspaces: []rundomain.WorkspaceID{workspace}})
	must(t, err)
	defer closeWorker()
	httpControl, closeHTTPControl, err := bootstrap.BuildProviderHTTPControlRuntime(ctx, bootstrap.ProviderHTTPControlRuntimeConfig{
		ExecutorDatabaseURL: executorDSN, ReconcilerDatabaseURL: reconcilerDSN,
		ProviderIDs: []string{"provider_g3_http", "provider_g3_agent"}, AllowedHosts: []string{httpAgentHost},
		TransportKinds: []string{supplydomain.TransportHTTP, supplydomain.TransportAgentHTTP}, AllowHTTP: true, AllowLoopback: true,
	}, secrets)
	must(t, err)
	defer closeHTTPControl()
	reconcilerPool, err := database.Open(ctx, reconcilerDSN)
	must(t, err)
	defer reconcilerPool.Close()
	mcpStatus, err := supplystatus.New(map[string]supply.ProviderStatusReader{"provider_g3_mcp": mcpRuntime.StatusReader()})
	must(t, err)
	mcpResults, err := runapp.NewProviderResults(runpg.NewProviderResults(reconcilerPool))
	must(t, err)
	mcpReconciler, err := runapp.NewProviderReconciler(runpg.NewProviderReconciliation(reconcilerPool), mcpStatus, mcpResults)
	must(t, err)

	dispatch := func(entry g3EntrySpec) {
		t.Helper()
		activated, err := control.Activate(ctx, workspace, []string{entry.deploymentID}, 1)
		must(t, err)
		if activated != 1 {
			t.Fatal("G3 matrix activation drift", entry.name, activated)
		}
		lease, err := control.LeaseOne(ctx, workspace, "worker_g3_matrix", 30*time.Second)
		must(t, err)
		var executor runapp.Executor = httpExecutor
		if entry.name == "mcp" {
			executor = mcpRuntime.Executor()
		}
		dispatcher, err := runapp.NewDispatcher(control, executor)
		must(t, err)
		record, err := dispatcher.Dispatch(ctx, lease)
		must(t, err)
		if string(record.Run.ID) != runs[entry.name] || record.Run.State != rundomain.Running || record.Attempt.State != rundomain.AttemptSubmitted || record.Attempt.ProviderID != entry.providerID {
			t.Fatal("G3 matrix adapter did not enter shared submitted lifecycle", entry.name, record)
		}
		if entry.name == "mcp" {
			result, err := mcpReconciler.ReconcileOne(ctx, workspace)
			must(t, err)
			if result.Run.State != rundomain.Succeeded {
				t.Fatal("G3 MCP result did not converge", result)
			}
			return
		}
		cycle, err := httpControl.ControlOne(ctx, workspace)
		must(t, err)
		if !cycle.ReconciliationHandled || cycle.CancellationHandled {
			t.Fatal("G3 HTTP/Agent result did not converge", entry.name, cycle)
		}
	}
	for _, entry := range entries {
		dispatch(entry)
	}

	for _, entry := range entries {
		runID := runs[entry.name]
		var runState, jobState, subjectID, toolsetID, artifactKind, artifactContent, providerID, requestID, taskID string
		must(t, owner.QueryRow(ctx, `SELECT r.state,j.state,a.subject_id,a.toolset_version_id,x.kind,x.content_json::text,ra.provider_id,ra.provider_request_id,coalesce(ra.external_task_id,'')
		 FROM execution.runs r JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(r.workspace_id,r.id)
		 JOIN execution.run_admissions a ON (a.workspace_id,a.run_id)=(r.workspace_id,r.id)
		 JOIN execution.artifacts x ON (x.workspace_id,x.run_id)=(r.workspace_id,r.id)
		 JOIN execution.run_attempts ra ON (ra.workspace_id,ra.run_id)=(r.workspace_id,r.id) AND ra.attempt_no=1
		 WHERE r.workspace_id=$1 AND r.id=$2`, workspace, runID).Scan(&runState, &jobState, &subjectID, &toolsetID, &artifactKind, &artifactContent, &providerID, &requestID, &taskID))
		if runState != "succeeded" || jobState != "finished" || subjectID != subject || toolsetID != toolset || artifactKind != "provider_result" || providerID != entry.providerID || requestID == "" || !strings.Contains(artifactContent, `"source"`) || (entry.name != "mcp" && taskID == "") {
			t.Fatal("G3 public execution truth drift", entry.name, runState, jobState, subjectID, toolsetID, artifactKind, providerID, requestID, taskID, artifactContent)
		}
		var settlementState string
		must(t, owner.QueryRow(ctx, `SELECT state FROM execution.settlement_jobs WHERE workspace_id=$1 AND run_id=$2`, workspace, runID).Scan(&settlementState))
		if settlementState != "pending" {
			t.Fatal("G3 terminal result missed settlement handoff", entry.name, settlementState)
		}
	}

	settlementRuntime, closeSettlement, err := bootstrap.BuildUsageSettlementRuntime(ctx, bootstrap.UsageSettlementRuntimeConfig{DatabaseURL: settlementDSN})
	must(t, err)
	defer closeSettlement()
	for range entries {
		_, err = settlementRuntime.SettleOne(ctx, workspace)
		must(t, err)
	}
	for _, entry := range entries {
		var charge int64
		var outcome, state string
		must(t, owner.QueryRow(ctx, `SELECT u.charged_micro,u.outcome,j.state FROM commerce.usage_settlements u JOIN execution.settlement_jobs j ON (j.workspace_id,j.run_id)=(u.workspace_id,u.run_id) WHERE u.workspace_id=$1 AND u.run_id=$2`, workspace, runs[entry.name]).Scan(&charge, &outcome, &state))
		if charge != 10 || outcome != "succeeded" || state != "finished" {
			t.Fatal("G3 settlement drifted by adapter", entry.name, charge, outcome, state)
		}
	}
	var consumed, reserved int64
	must(t, owner.QueryRow(ctx, `SELECT consumed_micro,reserved_micro FROM commerce.budget_periods WHERE workspace_id=$1 AND budget_id='budget_g3_matrix' AND period_id='period_g3_matrix'`, workspace).Scan(&consumed, &reserved))
	if consumed != 30 || reserved != 0 {
		t.Fatal("G3 adapter matrix did not converge to one quota truth", consumed, reserved)
	}

	for _, entry := range entries {
		runID := runs[entry.name]
		for _, path := range []string{
			"/api/v1/workspaces/" + workspace + "/runs/" + runID,
			"/api/v1/workspaces/" + workspace + "/runs/" + runID + "/artifacts/art_" + runID,
		} {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.Header.Set("Authorization", "Bearer "+createKey)
			w := httptest.NewRecorder()
			api.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatal("G3 protected read failed", entry.name, path, w.Code, w.Body.String())
			}
			for _, internal := range []string{entry.providerID, "request-g3", "task-g3", "integration-secret-value", "provider_request_id", "external_task_id"} {
				if strings.Contains(w.Body.String(), internal) {
					t.Fatal("G3 public projection leaked adapter-specific evidence", entry.name, internal, w.Body.String())
				}
			}
		}
	}
	cross := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/"+workspace+"/runs/"+runs["http"], nil)
	cross.Header.Set("Authorization", "Bearer "+otherWorkspaceKey)
	crossW := httptest.NewRecorder()
	api.ServeHTTP(crossW, cross)
	if crossW.Code != http.StatusForbidden {
		t.Fatal("G3 matrix cross-workspace credential reached Run", crossW.Code, crossW.Body.String())
	}

	t.Log("G3 real PostgreSQL entry matrix verified: one workspace/subject, HTTP + upstream MCP + remote Agent, shared Admission/Governance/Run/Artifact/settlement truth, no adapter-handle leakage")
}
