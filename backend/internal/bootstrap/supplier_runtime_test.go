package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	execapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	execdomain "github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

type bootstrapSecretProvider struct{}

func (bootstrapSecretProvider) ResolveSecret(context.Context, supplyapp.SecretRequest) (supplyapp.Secret, error) {
	return supplyapp.NewSecret([]byte("not-used"))
}

func TestSupplierMCPRuntimeRejectsUnsafeDatabaseRoleCompositionBeforeConnecting(t *testing.T) {
	base := SupplierMCPRuntimeConfig{
		WorkerDatabaseURL:    "postgres://worker:pw@127.0.0.1:5432/mender?sslmode=disable",
		ConnectorDatabaseURL: "postgres://worker:pw@127.0.0.1:5432/mender?sslmode=disable",
		AllowedHosts:         []string{"mcp.example"},
	}
	if runtime, closeIt, err := BuildSupplierMCPRuntime(context.Background(), base, bootstrapSecretProvider{}); err == nil || runtime != nil || closeIt != nil || !strings.Contains(err.Error(), "distinct restricted role") {
		t.Fatal(runtime != nil, closeIt != nil, err)
	}
	base.ConnectorDatabaseURL = "postgres://mcp_connector:pw@127.0.0.1:5432/other?sslmode=disable"
	if runtime, closeIt, err := BuildSupplierMCPRuntime(context.Background(), base, bootstrapSecretProvider{}); err == nil || runtime != nil || closeIt != nil || !strings.Contains(err.Error(), "same database") {
		t.Fatal(runtime != nil, closeIt != nil, err)
	}
	base.ConnectorDatabaseURL = "postgres://mcp_connector:pw@127.0.0.1:5432/mender?sslmode=disable"
	if runtime, closeIt, err := BuildSupplierMCPRuntime(context.Background(), base, nil); err == nil || runtime != nil || closeIt != nil {
		t.Fatal("nil MCP secret provider was accepted", runtime != nil, closeIt != nil, err)
	}
}

type fakeControlCancel struct {
	err   error
	calls int
}

func (f *fakeControlCancel) CancelOne(context.Context, execdomain.WorkspaceID) (execapp.ProviderResultRecord, error) {
	f.calls++
	return execapp.ProviderResultRecord{}, f.err
}

type fakeControlStatus struct {
	err   error
	calls int
}

func (f *fakeControlStatus) ReconcileOne(context.Context, execdomain.WorkspaceID) (execapp.ProviderResultRecord, error) {
	f.calls++
	return execapp.ProviderResultRecord{}, f.err
}

func TestReviewedProviderControlCycleIsBoundedAndUnknownCancelFallsThroughToStatus(t *testing.T) {
	cancel := &fakeControlCancel{err: execapp.ErrProviderCancelOutcomeUnknown}
	status := &fakeControlStatus{err: execapp.ErrNoProviderReconciliation}
	runtime, err := NewReviewedProviderControlRuntime(cancel, status)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.ControlOne(context.Background(), "ws_control")
	if err != nil || cancel.calls != 1 || status.calls != 1 || !result.CancellationHandled || !result.CancellationOutcomeUnknown || result.ReconciliationHandled {
		t.Fatal(result, cancel.calls, status.calls, err)
	}
	cancel.err, status.err = execapp.ErrNoProviderCancellation, nil
	result, err = runtime.ControlOne(context.Background(), "ws_control")
	if err != nil || cancel.calls != 2 || status.calls != 2 || result.CancellationHandled || result.CancellationOutcomeUnknown || !result.ReconciliationHandled {
		t.Fatal(result, cancel.calls, status.calls, err)
	}
}

func TestReviewedProviderControlCycleFailsClosed(t *testing.T) {
	if runtime, err := NewReviewedProviderControlRuntime(nil, &fakeControlStatus{}); err == nil || runtime != nil {
		t.Fatal("nil cancellation runner accepted")
	}
	cancel := &fakeControlCancel{err: errors.New("storage failure")}
	status := &fakeControlStatus{}
	runtime, err := NewReviewedProviderControlRuntime(cancel, status)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.ControlOne(context.Background(), "invalid workspace"); err == nil || cancel.calls != 0 || status.calls != 0 {
		t.Fatal("invalid workspace reached provider control", err, cancel.calls, status.calls)
	}
	if _, err = runtime.ControlOne(context.Background(), "ws_control"); err == nil || cancel.calls != 1 || status.calls != 0 {
		t.Fatal("cancel control failure did not stop the cycle", err, cancel.calls, status.calls)
	}
	ctx, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	if _, err = runtime.ControlOne(ctx, "ws_control"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("caller cancellation was not propagated", err)
	}
}

func TestProviderControlRuntimeRejectsUnsafeCompositionBeforeConnecting(t *testing.T) {
	base := ProviderHTTPControlRuntimeConfig{
		ExecutorDatabaseURL:   "postgres://executor:pw@127.0.0.1:5432/mender?sslmode=disable",
		ReconcilerDatabaseURL: "postgres://executor:pw@127.0.0.1:5432/mender?sslmode=disable",
		ProviderIDs:           []string{"provider_a"}, AllowedHosts: []string{"supplier.example"},
	}
	if runtime, closeIt, err := BuildProviderHTTPControlRuntime(context.Background(), base, bootstrapSecretProvider{}); err == nil || runtime != nil || closeIt != nil || !strings.Contains(err.Error(), "distinct restricted roles") {
		t.Fatal(runtime != nil, closeIt != nil, err)
	}
	base.ReconcilerDatabaseURL = "postgres://reconciler:pw@127.0.0.1:5432/other?sslmode=disable"
	if runtime, closeIt, err := BuildProviderHTTPControlRuntime(context.Background(), base, bootstrapSecretProvider{}); err == nil || runtime != nil || closeIt != nil || !strings.Contains(err.Error(), "same database") {
		t.Fatal(runtime != nil, closeIt != nil, err)
	}
	base.ReconcilerDatabaseURL = "postgres://reconciler:pw@127.0.0.1:5432/mender?sslmode=disable"
	if runtime, closeIt, err := BuildProviderHTTPControlRuntime(context.Background(), base, nil); err == nil || runtime != nil || closeIt != nil {
		t.Fatal("nil provider-control secret source was accepted")
	}
	for _, providers := range [][]string{nil, {"bad provider"}, {"provider_a", "provider_a"}} {
		candidate := base
		candidate.ProviderIDs = providers
		if runtime, closeIt, err := BuildProviderHTTPControlRuntime(context.Background(), candidate, bootstrapSecretProvider{}); err == nil || runtime != nil || closeIt != nil {
			t.Fatal("invalid reviewed provider list reached database composition", providers, runtime != nil, closeIt != nil, err)
		}
	}
}

func TestSupplierHTTPRuntimeRejectsUnsafeDatabaseRoleCompositionBeforeConnecting(t *testing.T) {
	base := SupplierHTTPRuntimeConfig{
		WorkerDatabaseURL:   "postgres://worker:pw@127.0.0.1:5432/mender?sslmode=disable",
		ExecutorDatabaseURL: "postgres://worker:pw@127.0.0.1:5432/mender?sslmode=disable",
		AllowedHosts:        []string{"supplier.example"},
	}
	if executor, closeIt, err := BuildSupplierHTTPExecutor(context.Background(), base, bootstrapSecretProvider{}); err == nil || executor != nil || closeIt != nil || !strings.Contains(err.Error(), "distinct restricted role") {
		t.Fatal(executor != nil, closeIt != nil, err)
	}
	base.ExecutorDatabaseURL = "postgres://executor:pw@127.0.0.1:5432/other?sslmode=disable"
	if executor, closeIt, err := BuildSupplierHTTPExecutor(context.Background(), base, bootstrapSecretProvider{}); err == nil || executor != nil || closeIt != nil || !strings.Contains(err.Error(), "same database") {
		t.Fatal(executor != nil, closeIt != nil, err)
	}
	base.ExecutorDatabaseURL = "postgres://executor:pw@127.0.0.1:5432/mender?sslmode=disable"
	if executor, closeIt, err := BuildSupplierHTTPExecutor(context.Background(), base, nil); err == nil || executor != nil || closeIt != nil {
		t.Fatal("nil secret provider was accepted", executor != nil, closeIt != nil, err)
	}
	for _, transports := range [][]string{{"unknown"}, {"agent_http", "agent_http"}, {"mcp_streamable_http"}} {
		candidate := base
		candidate.TransportKinds = transports
		if executor, closeIt, err := BuildSupplierHTTPExecutor(context.Background(), candidate, bootstrapSecretProvider{}); err == nil || executor != nil || closeIt != nil {
			t.Fatal("invalid reviewed HTTP/Agent transport set reached database composition", transports, executor != nil, closeIt != nil, err)
		}
	}
}
