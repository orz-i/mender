package bootstrap

import (
	"context"
	"errors"
	"sync"

	connfacade "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/inbound/facade"
	connpg "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/outbound/postgres"
	connapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	execfacade "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/inbound/facade"
	execpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/supplycancel"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/supplyexecutor"
	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/supplystatus"
	execapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	execdomain "github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	connectioncredentials "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/connectioncredentials"
	executioninput "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/executioninput"
	httpexecutor "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/http"
	supplypg "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/postgres"
	supplyapp "github.com/orz-i/mender/backend/internal/contexts/supply/application"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type SupplierHTTPRuntimeConfig struct {
	WorkerDatabaseURL   string
	ExecutorDatabaseURL string
	AllowedHosts        []string
	AllowHTTP           bool
	AllowLoopback       bool
}

type ProviderHTTPControlRuntimeConfig struct {
	ExecutorDatabaseURL   string
	ReconcilerDatabaseURL string
	ProviderIDs           []string
	AllowedHosts          []string
	AllowHTTP             bool
	AllowLoopback         bool
}

func sameDatabaseDistinctRoles(firstURL, secondURL string) error {
	firstConfig, err := database.Config(firstURL)
	if err != nil {
		return err
	}
	secondConfig, err := database.Config(secondURL)
	if err != nil {
		return err
	}
	first, second := firstConfig.ConnConfig, secondConfig.ConnConfig
	if first.Host != second.Host || first.Port != second.Port || first.Database != second.Database || first.User == second.User {
		return errors.New("runtime requires the same database with distinct restricted roles")
	}
	if len(first.Fallbacks) != len(second.Fallbacks) {
		return errors.New("runtime requires identical database failover targets")
	}
	for index := range first.Fallbacks {
		if first.Fallbacks[index].Host != second.Fallbacks[index].Host || first.Fallbacks[index].Port != second.Fallbacks[index].Port {
			return errors.New("runtime requires identical database failover targets")
		}
	}
	return nil
}

// BuildSupplierHTTPExecutor assembles the read-only execution-material plane
// separately from the Worker lease-control connection. SecretProvider remains
// an explicit injected capability; bootstrap never reads secrets from process
// environment or from execution tables.
func BuildSupplierHTTPExecutor(ctx context.Context, c SupplierHTTPRuntimeConfig, secrets supplyapp.SecretProvider) (execapp.Executor, func(), error) {
	if secrets == nil || c.WorkerDatabaseURL == "" || c.ExecutorDatabaseURL == "" {
		return nil, nil, errors.New("supplier HTTP runtime is not safely configured")
	}
	if err := sameDatabaseDistinctRoles(c.WorkerDatabaseURL, c.ExecutorDatabaseURL); err != nil {
		return nil, nil, errors.New("executor runtime requires the same database with a distinct restricted role")
	}
	pool, err := database.Open(ctx, c.ExecutorDatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (execapp.Executor, func(), error) {
		pool.Close()
		return nil, nil, err
	}
	if err = migrations.Verify(ctx, pool); err != nil {
		return fail(err)
	}
	if err = database.ExecutorRole(ctx, pool); err != nil {
		return fail(err)
	}
	inputService, err := execapp.NewRuntimeInputService(execpg.NewRuntimeInputs(pool))
	if err != nil {
		return fail(err)
	}
	credentialService, err := connapp.NewRuntimeService(connpg.NewRuntimeRepository(pool))
	if err != nil {
		return fail(err)
	}
	broker, err := supplyapp.NewBroker(
		executioninput.New(execfacade.NewRuntimeInputs(inputService)),
		connectioncredentials.New(connfacade.NewRuntimeCredentials(credentialService)),
		supplypg.NewDeployments(pool),
		secrets,
	)
	if err != nil {
		return fail(err)
	}
	supplier, err := httpexecutor.New(broker, httpexecutor.EgressPolicy{
		AllowedHosts:  c.AllowedHosts,
		AllowHTTP:     c.AllowHTTP,
		AllowLoopback: c.AllowLoopback,
	}, nil, nil, nil)
	if err != nil {
		return fail(err)
	}
	executor, err := supplyexecutor.New(supplier)
	if err != nil {
		return fail(err)
	}
	return executor, pool.Close, nil
}

type providerStatusRunner interface {
	ReconcileOne(context.Context, execdomain.WorkspaceID) (execapp.ProviderResultRecord, error)
}

type providerCancelRunner interface {
	CancelOne(context.Context, execdomain.WorkspaceID) (execapp.ProviderResultRecord, error)
}

type ProviderControlCycleResult struct {
	CancellationHandled        bool
	CancellationOutcomeUnknown bool
	ReconciliationHandled      bool
}

// ReviewedProviderControlRuntime deliberately has no autonomous background
// loop. A reviewed host chooses workspaces and cadence; each cycle performs at
// most one durable cancellation claim and one read-only status query.
type ReviewedProviderControlRuntime struct {
	cancel    providerCancelRunner
	reconcile providerStatusRunner
}

func NewReviewedProviderControlRuntime(cancel providerCancelRunner, reconcile providerStatusRunner) (*ReviewedProviderControlRuntime, error) {
	if cancel == nil || reconcile == nil {
		return nil, errors.New("reviewed provider control runtime requires cancellation and reconciliation")
	}
	return &ReviewedProviderControlRuntime{cancel: cancel, reconcile: reconcile}, nil
}

func (r *ReviewedProviderControlRuntime) ControlOne(ctx context.Context, workspace execdomain.WorkspaceID) (ProviderControlCycleResult, error) {
	if r == nil || r.cancel == nil || r.reconcile == nil || !workspace.IsValid() {
		return ProviderControlCycleResult{}, errors.New("provider control runtime is not safely configured")
	}
	if err := ctx.Err(); err != nil {
		return ProviderControlCycleResult{}, err
	}
	var result ProviderControlCycleResult
	_, cancelErr := r.cancel.CancelOne(ctx, workspace)
	switch {
	case cancelErr == nil:
		result.CancellationHandled = true
	case errors.Is(cancelErr, execapp.ErrNoProviderCancellation):
	case errors.Is(cancelErr, execapp.ErrProviderCancelOutcomeUnknown):
		result.CancellationHandled = true
		result.CancellationOutcomeUnknown = true
	case errors.Is(cancelErr, context.Canceled), errors.Is(cancelErr, context.DeadlineExceeded):
		return result, cancelErr
	default:
		return result, cancelErr
	}
	_, reconcileErr := r.reconcile.ReconcileOne(ctx, workspace)
	switch {
	case reconcileErr == nil:
		result.ReconciliationHandled = true
		return result, nil
	case errors.Is(reconcileErr, execapp.ErrNoProviderReconciliation):
		return result, nil
	default:
		return result, reconcileErr
	}
}

func reviewedProviderIDs(values []string, reader supply.ProviderStatusReader, canceler supply.ProviderCanceler) (map[string]supply.ProviderStatusReader, map[string]supply.ProviderCanceler, error) {
	if len(values) == 0 || len(values) > 256 || reader == nil || canceler == nil {
		return nil, nil, errors.New("provider control runtime requires reviewed providers")
	}
	readers := make(map[string]supply.ProviderStatusReader, len(values))
	cancelers := make(map[string]supply.ProviderCanceler, len(values))
	for _, providerID := range values {
		if _, duplicate := readers[providerID]; duplicate {
			return nil, nil, errors.New("provider control runtime contains duplicate provider")
		}
		// The Execution ACL constructors below validate provider IDs using the
		// same durable control-ID contract consumed by reconciliation targets.
		readers[providerID], cancelers[providerID] = reader, canceler
	}
	if _, err := supplystatus.New(readers); err != nil {
		return nil, nil, errors.New("provider control runtime contains invalid provider")
	}
	if _, err := supplycancel.New(cancelers); err != nil {
		return nil, nil, errors.New("provider control runtime contains invalid provider")
	}
	return readers, cancelers, nil
}

// BuildProviderHTTPControlRuntime composes two intentionally different DB
// principals: executor reads immutable admission/deployment/credential refs;
// reconciler reads/writes only provider control evidence and terminal state.
// SecretProvider and egress policy remain explicit reviewed capabilities.
func BuildProviderHTTPControlRuntime(ctx context.Context, c ProviderHTTPControlRuntimeConfig, secrets supplyapp.SecretProvider) (*ReviewedProviderControlRuntime, func(), error) {
	if secrets == nil || c.ExecutorDatabaseURL == "" || c.ReconcilerDatabaseURL == "" {
		return nil, nil, errors.New("provider HTTP control runtime is not safely configured")
	}
	if len(c.ProviderIDs) == 0 || len(c.ProviderIDs) > 256 {
		return nil, nil, errors.New("provider control runtime requires reviewed providers")
	}
	seenProviders := map[string]bool{}
	for _, providerID := range c.ProviderIDs {
		probe := execapp.ProviderTarget{WorkspaceID: "ws_probe", RunID: "run_probe", AttemptNo: 1, ProviderID: providerID, ProviderRequestID: "request/probe"}
		if !probe.Valid() || seenProviders[providerID] {
			return nil, nil, errors.New("provider control runtime contains invalid or duplicate provider")
		}
		seenProviders[providerID] = true
	}
	if err := sameDatabaseDistinctRoles(c.ExecutorDatabaseURL, c.ReconcilerDatabaseURL); err != nil {
		return nil, nil, errors.New("provider control runtime requires the same database with distinct restricted roles")
	}
	executorPool, err := database.Open(ctx, c.ExecutorDatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	closeExecutor := true
	defer func() {
		if closeExecutor {
			executorPool.Close()
		}
	}()
	if err = migrations.Verify(ctx, executorPool); err != nil {
		return nil, nil, err
	}
	if err = database.ExecutorRole(ctx, executorPool); err != nil {
		return nil, nil, err
	}
	reconcilerPool, err := database.Open(ctx, c.ReconcilerDatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	closeReconciler := true
	defer func() {
		if closeReconciler {
			reconcilerPool.Close()
		}
	}()
	if err = migrations.Verify(ctx, reconcilerPool); err != nil {
		return nil, nil, err
	}
	if err = database.ReconcilerRole(ctx, reconcilerPool); err != nil {
		return nil, nil, err
	}
	inputService, err := execapp.NewRuntimeInputService(execpg.NewRuntimeInputs(executorPool))
	if err != nil {
		return nil, nil, err
	}
	credentialService, err := connapp.NewRuntimeService(connpg.NewRuntimeRepository(executorPool))
	if err != nil {
		return nil, nil, err
	}
	broker, err := supplyapp.NewBroker(
		executioninput.New(execfacade.NewRuntimeInputs(inputService)),
		connectioncredentials.New(connfacade.NewRuntimeCredentials(credentialService)),
		supplypg.NewDeployments(executorPool), secrets,
	)
	if err != nil {
		return nil, nil, err
	}
	httpControl, err := httpexecutor.New(broker, httpexecutor.EgressPolicy{AllowedHosts: c.AllowedHosts, AllowHTTP: c.AllowHTTP, AllowLoopback: c.AllowLoopback}, nil, nil, nil)
	if err != nil {
		return nil, nil, err
	}
	readers, cancelers, err := reviewedProviderIDs(c.ProviderIDs, httpControl, httpControl)
	if err != nil {
		return nil, nil, err
	}
	statusSource, err := supplystatus.New(readers)
	if err != nil {
		return nil, nil, err
	}
	cancelSource, err := supplycancel.New(cancelers)
	if err != nil {
		return nil, nil, err
	}
	results, err := execapp.NewProviderResults(execpg.NewProviderResults(reconcilerPool))
	if err != nil {
		return nil, nil, err
	}
	reconcile, err := execapp.NewProviderReconciler(execpg.NewProviderReconciliation(reconcilerPool), statusSource, results)
	if err != nil {
		return nil, nil, err
	}
	cancel, err := execapp.NewProviderCancelDispatcher(execpg.NewProviderCancellations(reconcilerPool), cancelSource, systemClock{})
	if err != nil {
		return nil, nil, err
	}
	runtime, err := NewReviewedProviderControlRuntime(cancel, reconcile)
	if err != nil {
		return nil, nil, err
	}
	var once sync.Once
	closeResources := func() {
		once.Do(func() {
			reconcilerPool.Close()
			executorPool.Close()
		})
	}
	closeExecutor, closeReconciler = false, false
	return runtime, closeResources, nil
}
