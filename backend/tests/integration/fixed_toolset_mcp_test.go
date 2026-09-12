//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	identityapp "github.com/orz-i/mender/backend/internal/contexts/identity/application"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

func connectFixedMCP(t *testing.T, endpoint, key string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "mender-fixed-integration", Version: "v0.0.1"}, nil)
	httpClient := &http.Client{Transport: integrationBearerTransport{base: http.DefaultTransport, token: key}}
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func exerciseFixedToolsetMCP(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN, createKey, readOnlyKey, otherWorkspaceKey string) {
	t.Helper()
	identityService, err := identityapp.NewService(identitypg.New(runtime), keycodec.Codec{}, cancellationClock{})
	must(t, err)
	principal, err := identityService.Authenticate(ctx, createKey)
	must(t, err)
	if err = identityService.Authorize(ctx, principal, "ws_a", "run:create"); err != nil {
		t.Fatal("Fixed Toolset create-key precondition is not authorized", principal, err)
	}
	admissionDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_fixed_admit_", migrations.GrantAdmission, database.AdmissionRole)
	cancellationDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_fixed_cancel_", migrations.GrantCancellation, database.CancellationRole)
	at := time.Now().UTC().Truncate(time.Microsecond)

	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO catalog.tool_versions(id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,published_at) VALUES('tool_fixed_v1','tool_fixed','1.0.0','provider_fixed','price_fixed_v1','deploy_fixed_v1','Company Search','Search reviewed company data.','{"type":"object","additionalProperties":false,"required":["query","large_integer"],"properties":{"query":{"type":"string","minLength":1},"large_integer":{"type":"integer"}}}'::jsonb,'{"type":"object","properties":{"items":{"type":"array"}}}'::jsonb,'read_only','safe_read',true,'published',$1)`, []any{at.Add(-time.Hour)}},
		{`INSERT INTO catalog.tool_versions(id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,published_at) VALUES('tool_hidden_v1','tool_hidden','1.0.0','provider_fixed','price_fixed_v1','deploy_fixed_v1','Hidden Tool','','{"type":"object"}'::jsonb,'{"type":"object"}'::jsonb,'read_only','safe_read',true,'published',$1)`, []any{at.Add(-time.Hour)}},
		{`INSERT INTO catalog.tool_versions(id,tool_id,version,provider_id,price_version_id,deployment_revision,title,description,input_schema,output_schema,side_effect,idempotency,mcp_publishable,state,published_at) VALUES('tool_unreviewed_v1','tool_unreviewed','1.0.0','provider_fixed','price_fixed_v1','deploy_fixed_v1','Unreviewed','','{"type":"object"}'::jsonb,'{"type":"object"}'::jsonb,'read_only','safe_read',false,'published',$1)`, []any{at.Add(-time.Hour)}},
		{`INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,mcp_name,mcp_exposed,state,published_at) VALUES('ws_a','set_fixed_v1','tool_fixed','1.0.0','tool_fixed_v1','budget_fixed','conn_fixed','company_search',true,'published',$1)`, []any{at.Add(-time.Hour)}},
		{`INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,mcp_name,mcp_exposed,state,published_at) VALUES('ws_a','set_fixed_v1','tool_unreviewed','1.0.0','tool_unreviewed_v1','budget_fixed','conn_fixed','unreviewed_tool',true,'published',$1)`, []any{at.Add(-time.Hour)}},
		{`INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,state,published_at) VALUES('ws_a','set_fixed_v1','tool_hidden','1.0.0','tool_hidden_v1','budget_fixed','published',$1)`, []any{at.Add(-time.Hour)}},
		{`INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at) VALUES('ws_a','conn_fixed','provider_fixed','secret_fixed_internal','active',1,$1,$2)`, []any{at.Add(-time.Hour), at.Add(time.Hour)}},
		{`INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at) VALUES('ws_a','conn_fixed','sa_a',true,$1,$2)`, []any{at.Add(-time.Hour), at.Add(45 * time.Minute)}},
		{`INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,charge_micro,billing_policy,starts_at,ends_at,active) VALUES('price_fixed_v1','tool_fixed_v1','USD',60,60,'fixed_success_only',$1,$2,true)`, []any{at.Add(-time.Hour), at.Add(time.Hour)}},
		{`INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro) VALUES('ws_a','budget_fixed','period_fixed','USD',$1,$2,1000)`, []any{at.Add(-time.Hour), at.Add(time.Hour)}},
	}
	for _, statement := range statements {
		_, err := owner.Exec(ctx, statement.sql, statement.args...)
		must(t, err)
	}

	// Publication names are stable and unique inside a Toolset; invalid aliases
	// are rejected by the database before they can enter discovery.
	if _, err := owner.Exec(ctx, `INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,mcp_name,mcp_exposed,state,published_at) VALUES('ws_a','set_fixed_v1','tool_duplicate','1.0.0','tool_duplicate_v1','budget_fixed','conn_fixed','company_search',true,'published',$1)`, at); err == nil {
		t.Fatal("duplicate direct MCP alias accepted")
	}
	if _, err := owner.Exec(ctx, `INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,connection_id,mcp_name,mcp_exposed,state,published_at) VALUES('ws_a','set_invalid_name','tool_invalid','1.0.0','tool_fixed_v1','budget_fixed','conn_fixed','Bad-Name',true,'published',$1)`, at); err == nil {
		t.Fatal("unstable MCP alias accepted")
	}

	h, closeAPI, err := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{
		RunAPIEnabled: true, RunReadAPIEnabled: true, StartRunAPIEnabled: true, CoordinatedCancelEnabled: true,
		MCPGatewayEnabled: true, MCPFixedToolsetEnabled: true,
		DatabaseURL: runtimeDSN, AdmissionDatabaseURL: admissionDSN, CancellationDatabaseURL: cancellationDSN,
		CursorSigningKey: []byte(strings.Repeat("f", 32)),
	})
	must(t, err)
	defer closeAPI()
	principal, err = identityService.Authenticate(ctx, createKey)
	must(t, err)
	if err = identityService.Authorize(ctx, principal, "ws_a", "run:create"); err != nil {
		t.Fatal("Fixed Toolset create-key lost run:create after API bootstrap", principal, err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	endpoint := ts.URL + "/mcp/v1/workspaces/ws_a/toolsets/set_fixed_v1"
	// The official v1.7.0 client owns the full 2026-07-28 initialize metadata
	// contract. Pre-SDK auth/workspace/protocol rejection is covered separately;
	// don't maintain a second hand-written initialize implementation in tests.
	session := connectFixedMCP(t, endpoint, createKey)
	listed, err := session.ListTools(ctx, nil)
	must(t, err)
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "company_search" {
		t.Fatal("Fixed Toolset exposed hidden/unreviewed tools", listed.Tools)
	}
	inputSchema, _ := json.Marshal(listed.Tools[0].InputSchema)
	if !strings.Contains(string(inputSchema), `"_mender"`) || !strings.Contains(string(inputSchema), `"query"`) {
		t.Fatal("published Tool schema missing business/control fields", string(inputSchema))
	}
	invalidBusiness := map[string]any{
		"query": 42, "large_integer": 7,
		"_mender": map[string]any{"idempotency_key": "fixed_direct_schema_invalid", "currency": "USD", "max_charge_micro": "100"},
	}
	invalid, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "company_search", Arguments: invalidBusiness})
	must(t, err)
	if !invalid.IsError || invalid.Content[0].(*mcp.TextContent).Text != "INVALID_ARGUMENT" {
		t.Fatal("Fixed Toolset MCP bypassed shared Admission schema validation", invalid)
	}
	var invalidAdmissions int
	must(t, owner.QueryRow(ctx, `SELECT count(*) FROM execution.run_admissions WHERE workspace_id='ws_a' AND idempotency_key='fixed_direct_schema_invalid'`).Scan(&invalidAdmissions))
	if invalidAdmissions != 0 {
		t.Fatal("schema-invalid Fixed Toolset call created durable admission state", invalidAdmissions)
	}

	args := map[string]any{
		"query": "fixed-result-marker", "large_integer": json.Number("9007199254740993"),
		"_mender": map[string]any{"idempotency_key": "fixed_direct_0001", "currency": "USD", "max_charge_micro": "100"},
	}
	called, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "company_search", Arguments: args})
	must(t, err)
	receipt := mcpText(t, called)
	runID, _ := receipt["run_id"].(string)
	if runID == "" || receipt["submission_state"] != "accepted" || receipt["reserved_micro"] != "60" || receipt["replayed"] != false {
		t.Fatal("unexpected Fixed Toolset receipt", receipt)
	}
	var toolVersion, toolsetVersion, connectionID, priceVersion, deployment, budgetID, canonical string
	var reserved int64
	must(t, owner.QueryRow(ctx, `SELECT tool_version_id,toolset_version_id,connection_id,price_version_id,deployment_revision,budget_id,reserved_micro,canonical_arguments FROM execution.run_admissions WHERE workspace_id='ws_a' AND run_id=$1`, runID).Scan(&toolVersion, &toolsetVersion, &connectionID, &priceVersion, &deployment, &budgetID, &reserved, &canonical))
	if toolVersion != "tool_fixed_v1" || toolsetVersion != "set_fixed_v1" || connectionID != "conn_fixed" || priceVersion != "price_fixed_v1" || deployment != "deploy_fixed_v1" || budgetID != "budget_fixed" || reserved != 60 || strings.Contains(canonical, "_mender") || !strings.Contains(canonical, "9007199254740993") || strings.Contains(canonical, "9007199254740992") {
		t.Fatal("direct MCP call changed server-owned plan or argument precision", toolVersion, toolsetVersion, connectionID, priceVersion, deployment, budgetID, reserved, canonical)
	}

	replayed, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "company_search", Arguments: args})
	must(t, err)
	replay := mcpText(t, replayed)
	if replay["run_id"] != runID || replay["replayed"] != true {
		t.Fatal("direct MCP retry did not reuse the accepted Run", replay)
	}
	changed := map[string]any{
		"query": "different", "large_integer": json.Number("9007199254740993"),
		"_mender": map[string]any{"idempotency_key": "fixed_direct_0001", "currency": "USD", "max_charge_micro": "100"},
	}
	conflict, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "company_search", Arguments: changed})
	must(t, err)
	if !conflict.IsError || conflict.Content[0].(*mcp.TextContent).Text != "IDEMPOTENCY_CONFLICT" {
		t.Fatal("changed direct MCP replay did not fail closed", conflict)
	}

	// The asynchronous receipt remains connected to the existing Run lifecycle.
	meta := connectMCP(t, ts.URL+"/mcp/v1/workspaces/ws_a", createKey)
	got, err := meta.CallTool(ctx, &mcp.CallToolParams{Name: "mender_run_get", Arguments: map[string]any{"run_id": runID}})
	must(t, err)
	if mcpText(t, got)["execution_state"] != "queued" {
		t.Fatal("direct Run cannot be read through existing meta tools")
	}
	canceled, err := meta.CallTool(ctx, &mcp.CallToolParams{Name: "mender_run_cancel", Arguments: map[string]any{"run_id": runID, "reason": "fixed Toolset integration stop"}})
	must(t, err)
	if mcpText(t, canceled)["execution_state"] != "canceled" {
		t.Fatal("direct Run cannot be canceled through existing semantics")
	}
	var held int64
	must(t, owner.QueryRow(ctx, `SELECT reserved_micro FROM commerce.budget_periods WHERE workspace_id='ws_a' AND budget_id='budget_fixed' AND period_id='period_fixed'`).Scan(&held))
	if held != 0 {
		t.Fatal("canceled direct Run retained reserved quota", held)
	}

	// Discovery is recomputed per stateless request: revocation or disabling
	// removes the business Tool immediately without stale MCP sessions.
	_, err = owner.Exec(ctx, `UPDATE connections.connections SET state='revoked' WHERE workspace_id='ws_a' AND id='conn_fixed'`)
	must(t, err)
	listed, err = session.ListTools(ctx, nil)
	must(t, err)
	if len(listed.Tools) != 0 {
		t.Fatal("revoked Connection remained discoverable", listed.Tools)
	}
	_, err = owner.Exec(ctx, `UPDATE connections.connections SET state='active' WHERE workspace_id='ws_a' AND id='conn_fixed'`)
	must(t, err)
	_, err = owner.Exec(ctx, `UPDATE catalog.tool_versions SET state='disabled' WHERE id='tool_fixed_v1'`)
	must(t, err)
	listed, err = session.ListTools(ctx, nil)
	must(t, err)
	if len(listed.Tools) != 0 {
		t.Fatal("disabled ToolVersion remained discoverable", listed.Tools)
	}
	_, err = owner.Exec(ctx, `UPDATE catalog.tool_versions SET state='published' WHERE id='tool_fixed_v1'`)
	must(t, err)

	// Discovery remains protocol-compatible for a valid Workspace credential,
	// but a caller without run:create receives an empty callable surface. The
	// actual tools/call path re-authorizes run:create again before admission.
	readOnly := connectFixedMCP(t, endpoint, readOnlyKey)
	readOnlyTools, err := readOnly.ListTools(ctx, nil)
	must(t, err)
	if len(readOnlyTools.Tools) != 0 {
		t.Fatal("credential without run:create discovered Fixed Toolset tools", readOnlyTools.Tools)
	}

	// Cross-workspace identity is rejected before SDK JSON-RPC handling.
	r := httptest.NewRequest(http.MethodPost, "/mcp/v1/workspaces/ws_a/toolsets/set_fixed_v1", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	r.Header.Set("Authorization", "Bearer "+otherWorkspaceKey)
	r.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "jsonrpc") {
		t.Fatal("cross-workspace credential reached Fixed Toolset SDK", w.Code, w.Body.String())
	}
}
