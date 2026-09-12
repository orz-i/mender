package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcphttp "github.com/orz-i/mender/backend/internal/processes/mcpbridge/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/processes/mcpbridge/application"
)

type directRegistry struct {
	items []application.DirectTool
	calls int
}

func TestFixedToolsetDiscoveryHidesToolsWhenCreateScopeIsForbidden(t *testing.T) {
	registry := &directRegistry{items: []application.DirectTool{reviewedDirectTool()}}
	fixed, err := application.NewFixed(
		authFunc(func(context.Context, string) (application.Caller, error) {
			return application.Caller{WorkspaceID: "ws_a", SubjectID: "sa_read", CredentialID: "key_read"}, nil
		}),
		authorizeFunc(func(context.Context, application.Caller, string) error { return application.ErrForbidden }),
		&captureStarter{},
		registry,
	)
	if err != nil {
		t.Fatal(err)
	}
	items, err := fixed.ListTools(context.Background(), application.Caller{WorkspaceID: "ws_a", SubjectID: "sa_read", CredentialID: "key_read"}, "set_sales_v1")
	if err != nil || len(items) != 0 {
		t.Fatal("forbidden caller received a discoverable direct Tool surface", items, err)
	}
}

type authorizeFunc func(context.Context, application.Caller, string) error

func (f authorizeFunc) Authorize(ctx context.Context, caller application.Caller, action string) error {
	return f(ctx, caller, action)
}

func (r *directRegistry) List(_ context.Context, _ application.Caller, _ string) ([]application.DirectTool, error) {
	return append([]application.DirectTool(nil), r.items...), nil
}

func (r *directRegistry) Resolve(_ context.Context, _ application.Caller, _ string, name string) (application.DirectTool, error) {
	r.calls++
	for _, item := range r.items {
		if item.Name == name {
			return item, nil
		}
	}
	return application.DirectTool{}, application.ErrNotFound
}

func reviewedDirectTool() application.DirectTool {
	return application.DirectTool{
		Name: "company_search", Title: "Company Search", Description: "Searches reviewed company data.",
		ToolID: "tool_company", ToolVersion: "1.0.0", ToolVersionID: "tool_company_v1", ConnectionID: "conn_company",
		InputSchema:  `{"type":"object","additionalProperties":false,"required":["query","large_integer"],"properties":{"query":{"type":"string","minLength":1},"large_integer":{"type":"integer"}}}`,
		OutputSchema: `{"type":"object","properties":{"items":{"type":"array"}}}`,
		SideEffect:   "read_only", Idempotency: "safe_read",
	}
}

func connectFixedTools(t *testing.T, starter application.Starter, registry application.DirectTools, token, workspace, toolset string) (*mcp.ClientSession, func()) {
	t.Helper()
	auth := authFunc(func(_ context.Context, value string) (application.Caller, error) {
		if value != "machine-secret" {
			return application.Caller{}, application.ErrUnauthenticated
		}
		return application.Caller{WorkspaceID: "ws_a", SubjectID: "sa_a", CredentialID: "key_a"}, nil
	})
	fixed, err := application.NewFixed(auth, authorizeFunc(func(_ context.Context, _ application.Caller, action string) error {
		if action != "run:create" {
			return application.ErrForbidden
		}
		return nil
	}), starter, registry)
	if err != nil {
		t.Fatal(err)
	}
	h, err := mcphttp.NewFixed(fixed)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	client := mcp.NewClient(&mcp.Implementation{Name: "fixed-toolset-test", Version: "v0.0.1"}, nil)
	httpClient := &http.Client{Transport: bearerTransport{base: http.DefaultTransport, token: token}}
	endpoint := ts.URL + "/mcp/v1/workspaces/" + workspace + "/toolsets/" + toolset
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		ts.Close()
		t.Fatal(err)
	}
	return session, func() { _ = session.Close(); ts.Close() }
}

func TestFixedToolsetListsPublishedBusinessSchemaAndStartsServerOwnedRun(t *testing.T) {
	starter := &captureStarter{}
	registry := &directRegistry{items: []application.DirectTool{reviewedDirectTool()}}
	session, close := connectFixedTools(t, starter, registry, "machine-secret", "ws_a", "set_sales_v1")
	defer close()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil || len(listed.Tools) != 1 {
		t.Fatal(listed, err)
	}
	tool := listed.Tools[0]
	if tool.Name != "company_search" || tool.Title != "Company Search" || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint || !tool.Annotations.IdempotentHint {
		t.Fatal("unexpected direct Tool projection", tool)
	}
	encoded, _ := json.Marshal(tool.InputSchema)
	for _, required := range []string{`"_mender"`, `"idempotency_key"`, `"max_charge_micro"`, `"query"`} {
		if !strings.Contains(string(encoded), required) {
			t.Fatal("published input schema missing field", required, string(encoded))
		}
	}
	if meta := tool.Meta; meta["mender/resultMode"] != "async_run_artifact" || meta["mender/declaredResultSchema"] == nil {
		t.Fatal("missing declared asynchronous result metadata", meta)
	}

	args := map[string]any{
		"query": "acme", "large_integer": json.Number("9007199254740993"),
		"_mender": map[string]any{"idempotency_key": "direct-operation-0001", "currency": "USD", "max_charge_micro": "100000"},
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "company_search", Arguments: args})
	if err != nil || result.IsError || starter.calls != 1 || registry.calls != 1 {
		t.Fatal(result, err, starter.calls, registry.calls)
	}
	if starter.input.ToolID != "tool_company" || starter.input.ToolVersion != "1.0.0" || starter.input.ToolsetVersionID != "set_sales_v1" || starter.input.ConnectionID != "conn_company" || starter.input.IdempotencyKey != "direct-operation-0001" {
		t.Fatal("client influenced server-owned routing", starter.input)
	}
	if strings.Contains(string(starter.input.Arguments), "_mender") || !strings.Contains(string(starter.input.Arguments), "9007199254740993") || strings.Contains(string(starter.input.Arguments), "9007199254740992") {
		t.Fatal("control metadata leaked or business precision changed", string(starter.input.Arguments))
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"submission_state":"accepted"`) || !strings.Contains(text, `"run_id":"run_meta"`) {
		t.Fatal("direct Tool did not return accurate async Run receipt", text)
	}
}

func TestFixedToolsetRejectsRoutingOverridesAndDelegatesBusinessSchemaToAdmission(t *testing.T) {
	starter := &captureStarter{}
	registry := &directRegistry{items: []application.DirectTool{reviewedDirectTool()}}
	session, close := connectFixedTools(t, starter, registry, "machine-secret", "ws_a", "set_sales_v1")
	defer close()
	base := map[string]any{"query": "acme", "large_integer": 7, "_mender": map[string]any{"idempotency_key": "direct-operation-0002", "currency": "USD", "max_charge_micro": "100000"}}
	base["connection_id"] = "conn_attacker"
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "company_search", Arguments: base})
	if err != nil || !result.IsError || starter.calls != 0 || result.Content[0].(*mcp.TextContent).Text != "INVALID_ARGUMENT" {
		t.Fatal("routing override reached Admission", result, err, starter.calls)
	}
	badBusiness := map[string]any{"query": 42, "large_integer": 7, "_mender": map[string]any{"idempotency_key": "direct-operation-0003", "currency": "USD", "max_charge_micro": "100000"}}
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{Name: "company_search", Arguments: badBusiness})
	if err != nil || result.IsError || starter.calls != 1 {
		t.Fatal("fixed MCP entrypoint did not delegate business arguments to shared Admission validation", result, err, starter.calls)
	}
	if !strings.Contains(string(starter.input.Arguments), `"query":42`) {
		t.Fatal("fixed MCP entrypoint changed business arguments before Admission", string(starter.input.Arguments))
	}
}

func TestFixedToolsetFailsClosedForUnrepresentablePublishedSchema(t *testing.T) {
	broken := reviewedDirectTool()
	broken.InputSchema = `{"$ref":"https://example.test/external-schema.json","type":"object"}`
	auth := authFunc(func(context.Context, string) (application.Caller, error) {
		return application.Caller{WorkspaceID: "ws_a", SubjectID: "sa_a", CredentialID: "key_a"}, nil
	})
	fixed, err := application.NewFixed(auth, authorizeFunc(func(context.Context, application.Caller, string) error { return nil }), &captureStarter{}, &directRegistry{items: []application.DirectTool{broken}})
	if err != nil {
		t.Fatal(err)
	}
	h, err := mcphttp.NewFixed(fixed)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/mcp/v1/workspaces/ws_a/toolsets/set_sales_v1", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	r.Header.Set("Authorization", "Bearer anything")
	r.Header.Set("Mcp-Protocol-Version", mcphttp.ProtocolVersion)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "MCP_TOOLSET_UNAVAILABLE") {
		t.Fatal("unrepresentable reviewed schema was partially exposed", w.Code, w.Body.String())
	}
}
