package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpclient "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/mcp"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type upstreamBroker struct {
	discovery  application.PreparedMCPDiscovery
	invocation application.PreparedInvocation
}

func TestUpstreamMCPResponseLimitFailsClosed(t *testing.T) {
	fixture := newUpstreamFixture(t)
	secret, _ := application.NewSecret([]byte("upstream-secret"))
	deployment := upstreamDeployment(fixture.server.URL)
	deployment.MaxResponseBytes = 64
	broker := &upstreamBroker{discovery: application.PreparedMCPDiscovery{
		WorkspaceID: "ws_a", Deployment: deployment,
		Credential: application.CredentialReference{ConnectionID: "conn_upstream", ProviderID: "provider_upstream", CredentialVersionRef: "secret_ref", Revision: 1, ValidUntil: time.Now().Add(time.Hour)},
		Secret:     secret,
	}}
	recorder := &snapshotRecorder{}
	client, err := mcpclient.New(broker, &routeRepo{}, recorder, &resultRepo{}, mcpclient.EgressPolicy{AllowedHosts: []string{"127.0.0.1"}, AllowHTTP: true, AllowLoopback: true}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Discover(context.Background(), application.MCPDiscoveryRef{WorkspaceID: "ws_a", SubjectID: "sa_a", ConnectionID: "conn_upstream", DeploymentRevision: "deploy_upstream"}); err == nil {
		t.Fatal("oversized upstream MCP response was accepted")
	}
	if len(recorder.items) != 0 {
		t.Fatal("oversized discovery response produced a durable snapshot")
	}
}

func (b *upstreamBroker) PrepareMCPDiscovery(context.Context, application.MCPDiscoveryRef, time.Time) (application.PreparedMCPDiscovery, error) {
	return b.discovery, nil
}
func (b *upstreamBroker) Prepare(context.Context, application.InvocationRef, time.Time) (application.PreparedInvocation, error) {
	return b.invocation, nil
}

type snapshotRecorder struct{ items []domain.MCPToolSnapshot }

func (r *snapshotRecorder) Record(_ context.Context, s domain.MCPToolSnapshot) error {
	r.items = append(r.items, s)
	return nil
}

type routeRepo struct{ route domain.MCPToolRoute }

func (r *routeRepo) ResolveMCPToolRoute(context.Context, string, string) (domain.MCPToolRoute, error) {
	return r.route, nil
}

type resultRepo struct{ items []application.MCPCallResult }

func (r *resultRepo) SaveMCPCallResult(_ context.Context, value application.MCPCallResult) error {
	r.items = append(r.items, value)
	return nil
}

type upstreamFixture struct {
	server   *httptest.Server
	tool     *mcp.Tool
	mu       sync.Mutex
	calls    int
	seenAuth []string
}

func newUpstreamFixture(t *testing.T) *upstreamFixture {
	t.Helper()
	f := &upstreamFixture{}
	f.tool = &mcp.Tool{Name: "company.search", Title: "Company Search", Description: "Searches\nreviewed upstream data.", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`), OutputSchema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`), Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}}
	server := mcp.NewServer(&mcp.Implementation{Name: "upstream-fixture", Version: "v0.0.1"}, &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}})
	server.AddTool(f.tool, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		f.mu.Lock()
		f.calls++
		f.mu.Unlock()
		if req == nil || req.Params == nil || req.Params.Name != "company.search" {
			t.Fatal("unexpected upstream call", req)
		}
		return &mcp.CallToolResult{StructuredContent: map[string]any{"ok": true, "source": "upstream"}}, nil
	})
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, PropagateRequestCancellation: true})
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seenAuth = append(f.seenAuth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer upstream-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		transport.ServeHTTP(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func upstreamDeployment(endpoint string) domain.Deployment {
	return domain.Deployment{Revision: "deploy_upstream", ProviderID: "provider_upstream", TransportKind: "mcp_streamable_http", EndpointURL: endpoint, HTTPMethod: "POST", AuthMode: "bearer", MCPProtocolVersion: "2026-07-28", MCPStateless: true, RequestTimeout: 3 * time.Second, MaxRequestBytes: 64 << 10, MaxResponseBytes: 1 << 20, State: "active", CreatedAt: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)}
}

func TestUpstreamMCPDiscoveryAndCallUseReviewedSecretAndExactSnapshot(t *testing.T) {
	fixture := newUpstreamFixture(t)
	secret, err := application.NewSecret([]byte("upstream-secret"))
	if err != nil {
		t.Fatal(err)
	}
	deployment := upstreamDeployment(fixture.server.URL)
	broker := &upstreamBroker{discovery: application.PreparedMCPDiscovery{WorkspaceID: "ws_a", Deployment: deployment, Credential: application.CredentialReference{ConnectionID: "conn_upstream", ProviderID: "provider_upstream", CredentialVersionRef: "secret_ref", Revision: 1, ValidUntil: time.Now().Add(time.Hour)}, Secret: secret}}
	recorder := &snapshotRecorder{}
	routes := &routeRepo{}
	results := &resultRepo{}
	client, err := mcpclient.New(broker, routes, recorder, results, mcpclient.EgressPolicy{AllowedHosts: []string{"127.0.0.1"}, AllowHTTP: true, AllowLoopback: true}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	items, err := client.Discover(context.Background(), application.MCPDiscoveryRef{WorkspaceID: "ws_a", SubjectID: "sa_a", ConnectionID: "conn_upstream", DeploymentRevision: "deploy_upstream"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(recorder.items) != 1 || items[0].ToolName != "company.search" || !strings.Contains(items[0].Description, "\n") {
		t.Fatal("unexpected discovery", items)
	}

	routes.route = domain.MCPToolRoute{ToolVersionID: "tool_upstream_v1", DeploymentRevision: "deploy_upstream", UpstreamToolName: "company.search", SnapshotSHA256: items[0].ContentSHA256, State: "active", CreatedAt: time.Now().UTC()}
	broker.invocation = application.PreparedInvocation{WorkspaceID: "ws_a", RunID: "run_upstream", ToolVersionID: "tool_upstream_v1", Deployment: deployment, CanonicalArguments: `{"query":"Acme"}`, Credential: broker.discovery.Credential, Secret: secret}
	out, err := client.Submit(context.Background(), supply.Submission{WorkspaceID: "ws_a", RunID: "run_upstream", AttemptNo: 1, Generation: 1, SubmissionKey: "mender.submit.run_upstream.1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Disposition != supply.Accepted || out.ProviderID != "provider_upstream" || len(results.items) != 1 || results.items[0].State != "succeeded" || !strings.Contains(results.items[0].ResultJSON, `"ok":true`) {
		t.Fatal("unexpected call outcome", out, results.items)
	}
	fixture.mu.Lock()
	calls := fixture.calls
	auth := append([]string(nil), fixture.seenAuth...)
	fixture.mu.Unlock()
	if calls != 1 {
		t.Fatal("upstream Tool called more than once", calls)
	}
	for _, value := range auth {
		if value != "Bearer upstream-secret" || strings.Contains(value, "platform") {
			t.Fatal("credential boundary failed", auth)
		}
	}
}

func TestUpstreamMCPCallFailsClosedOnSchemaDriftBeforeToolExecution(t *testing.T) {
	fixture := newUpstreamFixture(t)
	secret, _ := application.NewSecret([]byte("upstream-secret"))
	deployment := upstreamDeployment(fixture.server.URL)
	broker := &upstreamBroker{invocation: application.PreparedInvocation{WorkspaceID: "ws_a", RunID: "run_drift", ToolVersionID: "tool_upstream_v1", Deployment: deployment, CanonicalArguments: `{"query":"Acme"}`, Credential: application.CredentialReference{ConnectionID: "conn_upstream", ProviderID: "provider_upstream", CredentialVersionRef: "secret_ref", Revision: 1, ValidUntil: time.Now().Add(time.Hour)}, Secret: secret}}
	routes := &routeRepo{route: domain.MCPToolRoute{ToolVersionID: "tool_upstream_v1", DeploymentRevision: "deploy_upstream", UpstreamToolName: "company.search", SnapshotSHA256: strings.Repeat("a", 64), State: "active", CreatedAt: time.Now().UTC()}}
	results := &resultRepo{}
	client, err := mcpclient.New(broker, routes, &snapshotRecorder{}, results, mcpclient.EgressPolicy{AllowedHosts: []string{"127.0.0.1"}, AllowHTTP: true, AllowLoopback: true}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Submit(context.Background(), supply.Submission{WorkspaceID: "ws_a", RunID: "run_drift", AttemptNo: 1, Generation: 1, SubmissionKey: "mender.submit.run_drift.1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Disposition != supply.Accepted || len(results.items) != 1 || results.items[0].ErrorCode != "MCP_SCHEMA_DRIFT" {
		t.Fatal("schema drift was not durably failed", out, results.items)
	}
	fixture.mu.Lock()
	calls := fixture.calls
	fixture.mu.Unlock()
	if calls != 0 {
		t.Fatal("schema drift reached tools/call", calls)
	}
}
