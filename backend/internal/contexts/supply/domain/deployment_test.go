package domain

import (
	"testing"
	"time"
)

func validAgentDeployment() Deployment {
	return Deployment{
		Revision: "deploy_agent_1", ProviderID: "provider_agent", TransportKind: TransportAgentHTTP,
		EndpointURL: "https://agent.example/v1/tasks", HTTPMethod: "POST",
		StatusEndpointURL: "https://agent.example/v1/tasks/status", StatusHTTPMethod: "POST",
		CancelEndpointURL: "https://agent.example/v1/tasks/cancel", CancelHTTPMethod: "POST",
		AuthMode: "bearer", IdempotencyHeader: "Idempotency-Key", RequestTimeout: time.Second,
		MaxRequestBytes: 65536, MaxResponseBytes: 1 << 20, State: "active", CreatedAt: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC),
	}
}

func TestHTTPDeploymentCannotClaimAgentSupplementalInputEndpoint(t *testing.T) {
	d := validAgentDeployment()
	d.TransportKind = TransportHTTP
	d.InputEndpointURL, d.InputHTTPMethod = "https://agent.example/v1/tasks/input", "POST"
	if d.Validate() == nil || d.SupportsSupplementalInput() {
		t.Fatal("ordinary HTTP deployment accepted an Agent supplemental-input endpoint")
	}
}

func TestRemoteAgentDeploymentRequiresAsyncControlContract(t *testing.T) {
	d := validAgentDeployment()
	if d.Validate() != nil || !d.SupportsRemoteAgent() {
		t.Fatal("valid remote Agent deployment rejected")
	}
	if d.SupportsSupplementalInput() {
		t.Fatal("remote Agent deployment without input endpoint claimed supplemental-input support")
	}
	d.InputEndpointURL, d.InputHTTPMethod = "https://agent.example/v1/tasks/input", "POST"
	if d.Validate() != nil || !d.SupportsSupplementalInput() {
		t.Fatal("reviewed remote Agent input endpoint was rejected")
	}
	d.InputHTTPMethod = "GET"
	if d.Validate() == nil || d.SupportsSupplementalInput() {
		t.Fatal("unsafe remote Agent input method was accepted")
	}
	d.StatusEndpointURL, d.StatusHTTPMethod = "", ""
	if d.Validate() == nil || d.SupportsRemoteAgent() {
		t.Fatal("remote Agent deployment without status endpoint accepted")
	}
	d = validAgentDeployment()
	d.CancelEndpointURL, d.CancelHTTPMethod = "", ""
	if d.Validate() == nil || d.SupportsRemoteAgent() {
		t.Fatal("remote Agent deployment without cancel endpoint accepted")
	}
}

func TestRemoteAgentDeploymentRejectsMCPAndUnsafeIdempotencyFields(t *testing.T) {
	d := validAgentDeployment()
	d.MCPProtocolVersion, d.MCPStateless = "2026-07-28", true
	if d.Validate() == nil {
		t.Fatal("remote Agent deployment accepted MCP protocol fields")
	}
	d = validAgentDeployment()
	d.IdempotencyHeader = "Authorization"
	if d.Validate() == nil {
		t.Fatal("remote Agent deployment accepted reserved idempotency header")
	}
}
