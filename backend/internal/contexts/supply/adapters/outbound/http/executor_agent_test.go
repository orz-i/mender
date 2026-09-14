package httpexecutor

import (
	"context"
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

func (b agentBroker) Prepare(_ context.Context, ref application.InvocationRef, _ time.Time) (application.PreparedInvocation, error) {
	return application.PreparedInvocation{WorkspaceID: ref.WorkspaceID, RunID: ref.RunID, ToolVersionID: "tv_agent", Deployment: b.deployment, CanonicalArguments: `{"prompt":"hello"}`}, nil
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
