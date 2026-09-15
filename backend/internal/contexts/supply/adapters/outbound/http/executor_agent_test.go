package httpexecutor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	"github.com/orz-i/mender/backend/internal/contexts/supply/domain"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type agentBroker struct {
	deployment domain.Deployment
}

func TestAgentHTTPStatusInputRequiredNeedsReviewedInputEndpoint(t *testing.T) {
	payload := []byte(`{"observation_id":"obs.input.1","state":"input_required","input_request":{"input_request_id":"input.req.1","prompt":"Choose a region","input_schema":{"type":"object","properties":{"region":{"type":"string"}},"required":["region"]}},"observed_at":"2026-09-15T04:00:00Z"}`)
	decoded, err := decodeStatus(payload)
	if err != nil || decoded.State != supply.StatusInputRequired || decoded.InputRequestID != "input.req.1" {
		t.Fatal("input-required status decoder rejected reviewed shape", decoded, err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.Error(w, "unexpected path", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	policy := EgressPolicy{AllowedHosts: []string{"127.0.0.1"}, AllowHTTP: true, AllowLoopback: true}
	query := supply.StatusQuery{WorkspaceID: "ws_agent", RunID: "run_agent", AttemptNo: 1, ProviderID: "provider_agent", ProviderRequestID: "request-agent", ExternalTaskID: "task-agent"}

	withoutInput, err := NewForTransports(agentBroker{deployment: agentDeployment(server.URL)}, policy, []string{domain.TransportAgentHTTP}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = withoutInput.QueryStatus(context.Background(), query); !errors.Is(err, supply.ErrProviderStatusUnavailable) {
		t.Fatal("agent status created input request without reviewed input endpoint", err)
	}

	deployment := agentDeployment(server.URL)
	deployment.InputEndpointURL, deployment.InputHTTPMethod = server.URL+"/input", "POST"
	if deployment.Validate() != nil || !deployment.SupportsSupplementalInput() {
		t.Fatal("reviewed Agent deployment did not retain supplemental-input capability", deployment.Validate())
	}
	withInput, err := NewForTransports(agentBroker{deployment: deployment}, policy, []string{domain.TransportAgentHTTP}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := withInput.QueryStatus(context.Background(), query)
	if err != nil || observation.State != supply.StatusInputRequired || observation.InputRequestID != "input.req.1" || observation.InputPrompt != "Choose a region" || observation.InputSchemaJSON == "" {
		t.Fatal("reviewed agent input-required status was not preserved", observation, err)
	}

	if _, err = decodeStatus([]byte(`{"observation_id":"obs.input.2","state":"input_required","input_request":{"input_request_id":"input.req.2","prompt":"x","input_schema":{"type":"object"},"unexpected":true},"observed_at":"2026-09-15T04:00:00Z"}`)); err == nil {
		t.Fatal("input-required status accepted extra input_request fields")
	}
}

func (b agentBroker) Prepare(_ context.Context, ref application.InvocationRef, _ time.Time) (application.PreparedInvocation, error) {
	return application.PreparedInvocation{WorkspaceID: ref.WorkspaceID, RunID: ref.RunID, ToolVersionID: "tv_agent", Deployment: b.deployment, CanonicalArguments: `{"prompt":"hello"}`}, nil
}

func (b agentBroker) PrepareControl(_ context.Context, ref application.InvocationRef, _ time.Time) (application.PreparedControl, error) {
	return application.PreparedControl{WorkspaceID: ref.WorkspaceID, RunID: ref.RunID, Deployment: b.deployment}, nil
}

func agentDeployment(serverURL string) domain.Deployment {
	return domain.Deployment{
		Revision: "deploy_agent", ProviderID: "provider_agent", TransportKind: domain.TransportAgentHTTP,
		EndpointURL: serverURL + "/submit", HTTPMethod: "POST", StatusEndpointURL: serverURL + "/status", StatusHTTPMethod: "POST",
		CancelEndpointURL: serverURL + "/cancel", CancelHTTPMethod: "POST", AuthMode: "none", IdempotencyHeader: "Idempotency-Key",
		RequestTimeout: time.Second, MaxRequestBytes: 4096, MaxResponseBytes: 4096, State: "active", CreatedAt: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC),
	}
}

func TestAgentHTTPSubmitRequiresExplicitTransportOptInAndExternalTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/submit" {
			http.Error(w, "unexpected path", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Header.Get("Idempotency-Key") {
		case "submit-agent-1":
			_, _ = w.Write([]byte(`{"provider_request_id":"request-agent","external_task_id":"task-agent"}`))
		case "submit-agent-2":
			_, _ = w.Write([]byte(`{"provider_request_id":"request-agent-without-task"}`))
		default:
			http.Error(w, "unexpected idempotency key", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	deployment := agentDeployment(server.URL)
	defaultExecutor, err := New(agentBroker{deployment: deployment}, EgressPolicy{AllowedHosts: []string{"127.0.0.1"}, AllowHTTP: true, AllowLoopback: true}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	request := supply.Submission{WorkspaceID: "ws_agent", RunID: "run_agent", AttemptNo: 1, Generation: 1, SubmissionKey: "submit-agent-1"}
	if _, err = defaultExecutor.Submit(context.Background(), request); err == nil {
		t.Fatal("default HTTP executor accepted remote Agent transport without explicit opt-in")
	}

	executor, err := NewForTransports(agentBroker{deployment: deployment}, EgressPolicy{AllowedHosts: []string{"127.0.0.1"}, AllowHTTP: true, AllowLoopback: true}, []string{domain.TransportAgentHTTP}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := executor.Submit(context.Background(), request)
	if err != nil || result.Disposition != supply.Accepted || result.ProviderRequestID != "request-agent" || result.ExternalTaskID != "task-agent" {
		t.Fatal("reviewed Agent submit did not preserve exact durable handles", result, err)
	}

	result, err = executor.Submit(context.Background(), supply.Submission{WorkspaceID: "ws_agent", RunID: "run_agent", AttemptNo: 2, Generation: 2, SubmissionKey: "submit-agent-2"})
	if err != nil || result.Disposition != supply.Unknown || result.ProviderRequestID != "" || result.ExternalTaskID != "" {
		t.Fatal("Agent acknowledgement without external task was not treated as unknown", result, err)
	}
}

func TestAgentHTTPTransportSetRejectsMCPUnknownAndDuplicates(t *testing.T) {
	broker := agentBroker{deployment: agentDeployment("http://127.0.0.1:1")}
	policy := EgressPolicy{AllowedHosts: []string{"127.0.0.1"}, AllowHTTP: true, AllowLoopback: true}
	for _, kinds := range [][]string{{}, {domain.TransportMCPStreamableHTTP}, {"unknown"}, {domain.TransportAgentHTTP, domain.TransportAgentHTTP}} {
		if _, err := NewForTransports(broker, policy, kinds, nil, nil, nil); err == nil {
			t.Fatal("invalid reviewed transport set accepted", strings.Join(kinds, ","))
		}
	}
}
