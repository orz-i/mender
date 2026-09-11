package supply_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	httpexecutor "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/http"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type httpControlBroker struct {
	prepared application.PreparedControl
}

func (b httpControlBroker) Prepare(context.Context, application.InvocationRef, time.Time) (application.PreparedInvocation, error) {
	return application.PreparedInvocation{}, application.ErrInvocationUnavailable
}

func (b httpControlBroker) PrepareControl(_ context.Context, ref application.InvocationRef, _ time.Time) (application.PreparedControl, error) {
	if ref.WorkspaceID != b.prepared.WorkspaceID || ref.RunID != b.prepared.RunID {
		return application.PreparedControl{}, application.ErrInvocationForbidden
	}
	return b.prepared, nil
}

func controlPrepared(t *testing.T, baseURL string, auth string) application.PreparedControl {
	t.Helper()
	invocation := httpPrepared(t, baseURL+"/submit", auth, 1<<20, time.Second)
	invocation.Deployment.StatusEndpointURL, invocation.Deployment.StatusHTTPMethod = baseURL+"/status", "POST"
	invocation.Deployment.CancelEndpointURL, invocation.Deployment.CancelHTTPMethod = baseURL+"/cancel", "POST"
	return application.PreparedControl{
		WorkspaceID: invocation.WorkspaceID, RunID: invocation.RunID, Deployment: invocation.Deployment,
		Credential: invocation.Credential, Secret: invocation.Secret,
	}
}

func newControlExecutor(t *testing.T, prepared application.PreparedControl, baseURL string) *httpexecutor.Executor {
	t.Helper()
	executor, err := httpexecutor.New(httpControlBroker{prepared: prepared}, localPolicy(t, baseURL), nil, nil, httpClock{at: time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func statusQuery() supply.StatusQuery {
	return supply.StatusQuery{WorkspaceID: "ws_http", RunID: "run_http", AttemptNo: 1, ProviderID: "provider_http", ProviderRequestID: "request/123", ExternalTaskID: "task/abc"}
}

func cancelQuery() supply.CancelQuery {
	return supply.CancelQuery{WorkspaceID: "ws_http", RunID: "run_http", AttemptNo: 1, ProviderID: "provider_http", ProviderRequestID: "request/123", ExternalTaskID: "task/abc", CancelKey: "mender.cancel.run_http.1"}
}

func TestHTTPProviderStatusUsesFixedEndpointAuthAndStrictResult(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/status" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer secret-http-token" || r.Header.Get("Idempotency-Key") != "" {
			t.Fatalf("unexpected status request: %s %s auth=%q idempotency=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key"))
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"provider_request_id":"request/123","external_task_id":"task/abc"}` || strings.Contains(string(body), "ws_http") || strings.Contains(string(body), "secret-http-token") {
			t.Fatal("status body leaked local execution facts", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"observation_id":"obs.status.1","state":"succeeded","result":{"ok":true},"observed_at":"2026-09-11T02:00:01.123456Z"}`)
	}))
	defer server.Close()

	prepared := controlPrepared(t, server.URL, "bearer")
	status, err := newControlExecutor(t, prepared, server.URL).QueryStatus(context.Background(), statusQuery())
	if err != nil || calls.Load() != 1 || status.State != supply.StatusSucceeded || status.ObservationID != "obs.status.1" || status.ResultJSON != `{"ok":true}` || !status.ObservedAt.Equal(time.Date(2026, 9, 11, 2, 0, 1, 123456000, time.UTC)) {
		t.Fatal(status, calls.Load(), err)
	}
}

func TestHTTPProviderCancelUsesDurableKeyAndAcknowledgement(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/cancel" || r.Header.Get("Idempotency-Key") != "mender.cancel.run_http.1" {
			t.Fatalf("cancel request did not preserve reviewed endpoint/key: %s %q", r.URL.Path, r.Header.Get("Idempotency-Key"))
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"provider_request_id":"request/123","external_task_id":"task/abc","cancel_key":"mender.cancel.run_http.1"}` {
			t.Fatal("unexpected cancel body", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"disposition":"acknowledged","observation_id":"obs.cancel.http.1","observed_at":"2026-09-11T02:00:02Z"}`)
	}))
	defer server.Close()

	result, err := newControlExecutor(t, controlPrepared(t, server.URL, "none"), server.URL).Cancel(context.Background(), cancelQuery())
	if err != nil || calls.Load() != 1 || result.Disposition != supply.CancelAcknowledged || result.ObservationID != "obs.cancel.http.1" {
		t.Fatal(result, calls.Load(), err)
	}
}

func TestHTTPProviderCancelTreatsTimeoutAndMalformedSuccessAsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		timeout time.Duration
	}{
		{"timeout", func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"disposition":"acknowledged","observation_id":"late","observed_at":"2026-09-11T02:00:02Z"}`)
		}, 100 * time.Millisecond},
		{"duplicate", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"disposition":"acknowledged","disposition":"unknown"}`)
		}, time.Second},
		{"unknown-field", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"disposition":"acknowledged","observation_id":"obs.cancel.bad","observed_at":"2026-09-11T02:00:02Z","extra":"unsafe"}`)
		}, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			prepared := controlPrepared(t, server.URL, "none")
			prepared.Deployment.RequestTimeout = tc.timeout
			result, err := newControlExecutor(t, prepared, server.URL).Cancel(context.Background(), cancelQuery())
			if err != nil || result.Disposition != supply.CancelUnknown {
				t.Fatal("ambiguous cancellation was not mapped to unknown", result, err)
			}
		})
	}
}

func TestHTTPProviderStatusFailsClosedOnRedirectMalformedAndProviderMismatch(t *testing.T) {
	targetCalls := atomic.Int32{}
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			http.Redirect(w, r, target.URL+"/secret", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer source.Close()
	prepared := controlPrepared(t, source.URL, "none")
	executor := newControlExecutor(t, prepared, source.URL)
	if _, err := executor.QueryStatus(context.Background(), statusQuery()); !errors.Is(err, supply.ErrProviderStatusUnavailable) || targetCalls.Load() != 0 {
		t.Fatal("status redirect was followed or not failed closed", err, targetCalls.Load())
	}

	prepared.Deployment.ProviderID = "provider_other"
	if _, err := newControlExecutor(t, prepared, source.URL).QueryStatus(context.Background(), statusQuery()); !errors.Is(err, supply.ErrProviderStatusUnavailable) {
		t.Fatal("provider mismatch reached control transport", err)
	}
}

func TestHTTPProviderControlCanReconcileDisabledDeploymentButRequiresDeclaredCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"observation_id":"obs.disabled.1","state":"pending","observed_at":"2026-09-11T02:00:03Z"}`)
	}))
	defer server.Close()
	prepared := controlPrepared(t, server.URL, "none")
	prepared.Deployment.State = "disabled"
	if _, err := newControlExecutor(t, prepared, server.URL).QueryStatus(context.Background(), statusQuery()); err != nil {
		t.Fatal("disabled deployment could not reconcile already-submitted work", err)
	}
	prepared.Deployment.StatusEndpointURL, prepared.Deployment.StatusHTTPMethod = "", ""
	if _, err := newControlExecutor(t, prepared, server.URL).QueryStatus(context.Background(), statusQuery()); !errors.Is(err, supply.ErrProviderStatusUnavailable) {
		t.Fatal("undeclared status capability was used", err)
	}
}
