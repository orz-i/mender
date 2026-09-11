//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/orz-i/mender/backend/internal/bootstrap"
	"github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/keycodec"
	identitypg "github.com/orz-i/mender/backend/internal/contexts/identity/adapters/outbound/postgres"
	identitydomain "github.com/orz-i/mender/backend/internal/contexts/identity/domain"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type integrationBearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t integrationBearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

func restrictedRoleDSN(t *testing.T, ctx context.Context, owner *pgxpool.Pool, baseDSN, prefix string, grant func(context.Context, *pgxpool.Pool, string) error, check func(context.Context, *pgxpool.Pool) error) string {
	t.Helper()
	_, suffix, password, err := (keycodec.Codec{}).Generate()
	must(t, err)
	role := prefix + suffix
	_, err = owner.Exec(ctx, "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD '"+password+"'")
	must(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = owner.Exec(cleanup, "DROP OWNED BY "+pgx.Identifier{role}.Sanitize())
		_, _ = owner.Exec(cleanup, "DROP ROLE "+pgx.Identifier{role}.Sanitize())
	})
	must(t, grant(ctx, owner, role))
	u, err := url.Parse(baseDSN)
	must(t, err)
	u.User = url.UserPassword(role, password)
	pool, err := database.Open(ctx, u.String())
	must(t, err)
	must(t, check(ctx, pool))
	pool.Close()
	return u.String()
}

func mcpText(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	if result == nil || result.IsError || len(result.Content) != 1 {
		t.Fatal("unexpected MCP tool result", result)
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("MCP tool result is not text", result.Content)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text.Text), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func connectMCP(t *testing.T, endpoint, key string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "mender-integration", Version: "v0.0.1"}, nil)
	httpClient := &http.Client{Transport: integrationBearerTransport{base: http.DefaultTransport, token: key}}
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	must(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func exerciseMCPGateway(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, runtimeDSN, createKey, readOnlyKey, otherWorkspaceKey string) {
	t.Helper()
	admissionDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_mcp_admit_", migrations.GrantAdmission, database.AdmissionRole)
	cancellationDSN := restrictedRoleDSN(t, ctx, owner, runtimeDSN, "mender_mcp_cancel_", migrations.GrantCancellation, database.CancellationRole)

	at := time.Now().UTC().Truncate(time.Microsecond)
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO catalog.tool_versions(id,tool_id,version,provider_id,price_version_id,deployment_revision,state,published_at) VALUES('tool_mcp_v1','tool_mcp','1.0.0','provider_mcp','price_mcp_v1','deploy_mcp_v1','published',$1)`, []any{at.Add(-time.Hour)}},
		{`INSERT INTO distribution.toolset_bindings(workspace_id,toolset_version_id,tool_id,tool_version_label,tool_version_id,budget_id,state,published_at) VALUES('ws_a','set_mcp_v1','tool_mcp','1.0.0','tool_mcp_v1','budget_mcp','published',$1)`, []any{at.Add(-time.Hour)}},
		{`INSERT INTO connections.connections(workspace_id,id,provider_id,credential_version_ref,state,revision,created_at,expires_at) VALUES('ws_a','conn_mcp','provider_mcp','secret_mcp_internal','active',1,$1,$2)`, []any{at.Add(-time.Hour), at.Add(time.Hour)}},
		{`INSERT INTO connections.connection_grants(workspace_id,connection_id,subject_id,active,created_at,expires_at) VALUES('ws_a','conn_mcp','sa_a',true,$1,$2)`, []any{at.Add(-time.Hour), at.Add(45 * time.Minute)}},
		{`INSERT INTO commerce.price_versions(id,tool_version_id,currency,reserve_micro,charge_micro,billing_policy,starts_at,ends_at,active) VALUES('price_mcp_v1','tool_mcp_v1','USD',40,40,'fixed_success_only',$1,$2,true)`, []any{at.Add(-time.Hour), at.Add(time.Hour)}},
		{`INSERT INTO commerce.budget_periods(workspace_id,budget_id,period_id,currency,starts_at,ends_at,limit_micro) VALUES('ws_a','budget_mcp','period_mcp','USD',$1,$2,1000)`, []any{at.Add(-time.Hour), at.Add(time.Hour)}},
	} {
		_, err := owner.Exec(ctx, statement.sql, statement.args...)
		must(t, err)
	}

	h, closeAPI, err := bootstrap.BuildAPI(ctx, bootstrap.APIConfig{
		RunAPIEnabled: true, RunReadAPIEnabled: true, StartRunAPIEnabled: true, CoordinatedCancelEnabled: true, MCPGatewayEnabled: true,
		DatabaseURL: runtimeDSN, AdmissionDatabaseURL: admissionDSN, CancellationDatabaseURL: cancellationDSN, CursorSigningKey: []byte(strings.Repeat("m", 32)),
	})
	must(t, err)
	defer closeAPI()
	ts := httptest.NewServer(h)
	defer ts.Close()

	session := connectMCP(t, ts.URL+"/mcp/v1/workspaces/ws_a", createKey)
	tools, err := session.ListTools(ctx, nil)
	must(t, err)
	if len(tools.Tools) != 4 || session.ID() != "" || session.InitializeResult().ProtocolVersion != "2026-07-28" {
		t.Fatal("unexpected MCP stateless discovery", len(tools.Tools), session.ID(), session.InitializeResult())
	}

	startArgs := map[string]any{
		"idempotency_key": "mcp_start_0001", "tool_id": "tool_mcp", "tool_version": "1.0.0", "toolset_id": "set_mcp_v1", "connection_id": "conn_mcp",
		"arguments": map[string]any{"query": "mcp-result-marker", "n": json.Number("9007199254740993")}, "currency": "USD", "max_charge_micro": "100",
	}
	started, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "mender_run_start", Arguments: startArgs})
	must(t, err)
	start := mcpText(t, started)
	runID, _ := start["run_id"].(string)
	if runID == "" || start["execution_state"] != "queued" || start["billing_state"] != "reserved" || start["reserved_micro"] != "40" || start["replayed"] != false {
		t.Fatal("unexpected MCP StartRun response", start)
	}
	var canonical string
	var held int64
	must(t, owner.QueryRow(ctx, `SELECT canonical_arguments FROM execution.run_admissions WHERE workspace_id='ws_a' AND run_id=$1`, runID).Scan(&canonical))
	must(t, owner.QueryRow(ctx, `SELECT reserved_micro FROM commerce.budget_periods WHERE workspace_id='ws_a' AND budget_id='budget_mcp' AND period_id='period_mcp'`).Scan(&held))
	if !strings.Contains(canonical, "9007199254740993") || strings.Contains(canonical, "9007199254740992") || held != 40 {
		t.Fatal("MCP StartRun bypassed exact canonical args or reservation", canonical, held)
	}

	replayed, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "mender_run_start", Arguments: startArgs})
	must(t, err)
	replay := mcpText(t, replayed)
	if replay["run_id"] != runID || replay["replayed"] != true {
		t.Fatal("MCP idempotent replay created a different Run", replay)
	}
	changed := map[string]any{}
	for key, value := range startArgs {
		changed[key] = value
	}
	changed["max_charge_micro"] = "101"
	conflict, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "mender_run_start", Arguments: changed})
	must(t, err)
	if !conflict.IsError || conflict.Content[0].(*mcp.TextContent).Text != "IDEMPOTENCY_CONFLICT" {
		t.Fatal("MCP changed replay did not fail closed", conflict)
	}

	got, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "mender_run_get", Arguments: map[string]any{"run_id": runID}})
	must(t, err)
	if state := mcpText(t, got)["execution_state"]; state != "queued" {
		t.Fatal(state)
	}
	canceled, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "mender_run_cancel", Arguments: map[string]any{"run_id": runID, "reason": "MCP integration stop"}})
	must(t, err)
	if state := mcpText(t, canceled)["execution_state"]; state != "canceled" {
		t.Fatal(state)
	}
	must(t, owner.QueryRow(ctx, `SELECT reserved_micro FROM commerce.budget_periods WHERE workspace_id='ws_a' AND budget_id='budget_mcp' AND period_id='period_mcp'`).Scan(&held))
	if held != 0 {
		t.Fatal("MCP cancellation did not release held quota", held)
	}

	readOnly := connectMCP(t, ts.URL+"/mcp/v1/workspaces/ws_a", readOnlyKey)
	denied, err := readOnly.CallTool(ctx, &mcp.CallToolParams{Name: "mender_run_start", Arguments: map[string]any{
		"idempotency_key": "mcp_readonly_0001", "tool_id": "tool_mcp", "tool_version": "1.0.0", "toolset_id": "set_mcp_v1", "connection_id": "conn_mcp", "arguments": map[string]any{}, "currency": "USD", "max_charge_micro": "100",
	}})
	must(t, err)
	if !denied.IsError || denied.Content[0].(*mcp.TextContent).Text != "FORBIDDEN" {
		t.Fatal(denied)
	}

	// Existing Artifact fixture proves the MCP bridge uses the same current
	// run:read authorization and never needs provider/control identifiers.
	rawResult, keyID, digest, err := (keycodec.Codec{}).Generate()
	must(t, err)
	must(t, identitypg.New(owner).Provision(ctx, identitydomain.Credential{ID: keyID, WorkspaceID: "ws_result", SubjectID: "sa_result_reader", Digest: digest, Scopes: []string{"run:read"}, CreatedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour)}))
	resultSession := connectMCP(t, ts.URL+"/mcp/v1/workspaces/ws_result", rawResult)
	artifact, err := resultSession.CallTool(ctx, &mcp.CallToolParams{Name: "mender_artifact_get", Arguments: map[string]any{"run_id": "run_result_success", "artifact_id": "art_run_result_success"}})
	must(t, err)
	artifactText := artifact.Content[0].(*mcp.TextContent).Text
	if artifact.IsError || !strings.Contains(artifactText, `42`) {
		t.Fatal("MCP Artifact read failed", artifact)
	}
	for _, hidden := range []string{"provider_request_id", "external_task_id", "source_observation_id", "canonical_arguments", digest} {
		if strings.Contains(artifactText, hidden) {
			t.Fatal("MCP leaked internal result evidence", hidden)
		}
	}

	// Cross-workspace identity is rejected before the SDK sees JSON-RPC.
	r := httptest.NewRequest(http.MethodPost, "/mcp/v1/workspaces/ws_a", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	r.Header.Set("Authorization", "Bearer "+otherWorkspaceKey)
	r.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "jsonrpc") {
		t.Fatal(w.Code, w.Body.String())
	}
}
