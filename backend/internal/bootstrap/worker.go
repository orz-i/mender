package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/heartbeat"
	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	rundomain "github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	settlementapp "github.com/orz-i/mender/backend/internal/processes/settlement/application"
	"github.com/orz-i/mender/backend/migrations"
)

type WorkerConfig struct {
	ControlEnabled      bool
	DispatchEnabled     bool
	DatabaseURL         string
	WorkerID            string
	Workspaces          []rundomain.WorkspaceID
	DeploymentRevisions []string
	PollInterval        time.Duration
	LeaseTTL            time.Duration
	HeartbeatInterval   time.Duration
	ActivationLimit     int
}

func LoadWorkerConfig(getenv func(string) string) (WorkerConfig, error) {
	c := WorkerConfig{PollInterval: time.Second, LeaseTTL: 30 * time.Second, HeartbeatInterval: 10 * time.Second, ActivationLimit: 10}
	switch getenv("MENDER_WORKER_DISPATCH_ENABLED") {
	case "", "false":
	case "true":
		c.DispatchEnabled = true
	default:
		return WorkerConfig{}, errors.New("MENDER_WORKER_DISPATCH_ENABLED must be true or false")
	}
	switch getenv("MENDER_WORKER_CONTROL_ENABLED") {
	case "", "false":
		if c.DispatchEnabled {
			return WorkerConfig{}, errors.New("worker dispatch requires worker control")
		}
		return c, nil
	case "true":
		c.ControlEnabled = true
	default:
		return WorkerConfig{}, errors.New("MENDER_WORKER_CONTROL_ENABLED must be true or false")
	}
	c.DatabaseURL = getenv("MENDER_WORKER_DATABASE_URL")
	c.WorkerID = strings.TrimSpace(getenv("MENDER_WORKER_ID"))
	if c.DatabaseURL == "" || c.WorkerID == "" {
		return WorkerConfig{}, errors.New("worker control requires database URL and worker ID")
	}
	if !validWorkerID(c.WorkerID) {
		return WorkerConfig{}, errors.New("invalid worker ID")
	}
	rawWorkspaces := strings.Split(getenv("MENDER_WORKER_WORKSPACES"), ",")
	if len(rawWorkspaces) == 0 || len(rawWorkspaces) > 64 {
		return WorkerConfig{}, errors.New("worker control requires 1-64 workspaces")
	}
	seen := map[string]bool{}
	for _, raw := range rawWorkspaces {
		value := strings.TrimSpace(raw)
		workspace := rundomain.WorkspaceID(value)
		if !workspace.IsValid() || seen[value] {
			return WorkerConfig{}, errors.New("invalid or duplicate worker workspace")
		}
		seen[value] = true
		c.Workspaces = append(c.Workspaces, workspace)
	}
	if raw := getenv("MENDER_WORKER_POLL_INTERVAL"); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil || interval < 100*time.Millisecond || interval > time.Minute {
			return WorkerConfig{}, errors.New("worker poll interval must be between 100ms and 1m")
		}
		c.PollInterval = interval
	}
	if c.DispatchEnabled {
		rawRevisions := strings.Split(getenv("MENDER_WORKER_DEPLOYMENT_REVISIONS"), ",")
		if len(rawRevisions) == 0 || len(rawRevisions) > 64 {
			return WorkerConfig{}, errors.New("worker dispatch requires 1-64 deployment revisions")
		}
		seenRevisions := map[string]bool{}
		for _, raw := range rawRevisions {
			value := strings.TrimSpace(raw)
			if !validWorkerID(value) || seenRevisions[value] {
				return WorkerConfig{}, errors.New("invalid or duplicate worker deployment revision")
			}
			seenRevisions[value] = true
			c.DeploymentRevisions = append(c.DeploymentRevisions, value)
		}
		if raw := getenv("MENDER_WORKER_LEASE_TTL"); raw != "" {
			value, err := time.ParseDuration(raw)
			if err != nil || value < 5*time.Second || value > 5*time.Minute {
				return WorkerConfig{}, errors.New("worker lease TTL must be between 5s and 5m")
			}
			c.LeaseTTL = value
		}
		if raw := getenv("MENDER_WORKER_HEARTBEAT_INTERVAL"); raw != "" {
			value, err := time.ParseDuration(raw)
			if err != nil || value < 100*time.Millisecond {
				return WorkerConfig{}, errors.New("worker heartbeat interval must be at least 100ms")
			}
			c.HeartbeatInterval = value
		}
		if c.HeartbeatInterval >= c.LeaseTTL/2 {
			return WorkerConfig{}, errors.New("worker heartbeat interval must be less than half the lease TTL")
		}
		if raw := getenv("MENDER_WORKER_ACTIVATION_LIMIT"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > 100 {
				return WorkerConfig{}, errors.New("worker activation limit must be between 1 and 100")
			}
			c.ActivationLimit = value
		}
	}
	return c, nil
}

func validWorkerID(v string) bool {
	if len(v) == 0 || len(v) > 128 {
		return false
	}
	for _, ch := range v {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func BuildWorkerControl(ctx context.Context, c WorkerConfig) (*runapp.WorkerControl, func(), error) {
	if !c.ControlEnabled || c.DatabaseURL == "" || !validWorkerID(c.WorkerID) || len(c.Workspaces) == 0 {
		return nil, nil, errors.New("worker control is not safely configured")
	}
	start, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := database.Open(start, c.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*runapp.WorkerControl, func(), error) {
		pool.Close()
		return nil, nil, err
	}
	if err = migrations.Verify(start, pool); err != nil {
		return fail(err)
	}
	if err = database.WorkerRole(start, pool); err != nil {
		return fail(err)
	}
	control, err := runapp.NewWorkerControl(runpg.NewWorkers(pool), systemClock{})
	if err != nil {
		return fail(err)
	}
	return control, pool.Close, nil
}

// ReviewedDispatchRuntime is intentionally constructed from an already-reviewed
// execution adapter. The default worker entrypoint never invents a secret source
// or supplier egress policy from ambient process state.
type ReviewedDispatchRuntime struct {
	executor runapp.Executor
}

func NewReviewedDispatchRuntime(executor runapp.Executor) (*ReviewedDispatchRuntime, error) {
	if executor == nil {
		return nil, errors.New("reviewed dispatch runtime requires an executor")
	}
	return &ReviewedDispatchRuntime{executor: executor}, nil
}

// ReviewedWorkerServices is an explicit capability set assembled by a trusted
// host. The default worker entrypoint still has no SecretProvider, supplier
// egress policy, provider-control runtime or settlement principal.
type ReviewedWorkerServices struct {
	dispatch        *ReviewedDispatchRuntime
	providerControl *ReviewedProviderControlRuntime
	settlement      *ReviewedUsageSettlementRuntime
}

func NewReviewedWorkerServices(dispatch *ReviewedDispatchRuntime, providerControl *ReviewedProviderControlRuntime, settlement *ReviewedUsageSettlementRuntime) (*ReviewedWorkerServices, error) {
	if dispatch == nil && providerControl == nil && settlement == nil {
		return nil, errors.New("reviewed worker services require at least one capability")
	}
	return &ReviewedWorkerServices{dispatch: dispatch, providerControl: providerControl, settlement: settlement}, nil
}

type ReviewedRuntimeCycleResult struct {
	ProviderControlCycles          int
	ProviderCancellationsHandled   int
	ProviderReconciliationsHandled int
	SettlementsHandled             int
}

// RunReviewedRuntimeCycle performs bounded background convergence. For every
// explicitly configured Workspace it makes at most one provider-control call
// and one settlement claim. It does not construct secrets, select tenants,
// create goroutines, or loop on its own.
func RunReviewedRuntimeCycle(ctx context.Context, logger *slog.Logger, workspaces []rundomain.WorkspaceID, services *ReviewedWorkerServices) (ReviewedRuntimeCycleResult, error) {
	var result ReviewedRuntimeCycleResult
	if services == nil || (services.providerControl == nil && services.settlement == nil) {
		return result, nil
	}
	if logger == nil || len(workspaces) == 0 || len(workspaces) > 64 {
		return result, errors.New("reviewed runtime cycle is not safely configured")
	}
	for _, workspace := range workspaces {
		if !workspace.IsValid() {
			return result, errors.New("reviewed runtime cycle contains invalid workspace")
		}
	}
	for _, workspace := range workspaces {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if services.providerControl != nil {
			cycle, err := services.providerControl.ControlOne(ctx, workspace)
			if err != nil {
				return result, err
			}
			result.ProviderControlCycles++
			if cycle.CancellationHandled {
				result.ProviderCancellationsHandled++
			}
			if cycle.ReconciliationHandled {
				result.ProviderReconciliationsHandled++
			}
			if cycle.CancellationHandled || cycle.ReconciliationHandled {
				logger.Info("provider control cycle handled durable work", "workspace_id", string(workspace), "cancellation_handled", cycle.CancellationHandled, "cancellation_outcome_unknown", cycle.CancellationOutcomeUnknown, "reconciliation_handled", cycle.ReconciliationHandled)
			}
		}
		if services.settlement != nil {
			receipt, err := services.settlement.SettleOne(ctx, string(workspace))
			switch {
			case err == nil:
				result.SettlementsHandled++
				logger.Info("usage settlement handled durable work", "workspace_id", string(workspace), "charged_micro", receipt.ChargedMicro)
			case errors.Is(err, settlementapp.ErrNoWork):
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				return result, err
			default:
				return result, err
			}
		}
	}
	return result, nil
}

func BuildDispatchSupervisor(control *runapp.WorkerControl, c WorkerConfig, runtime *ReviewedDispatchRuntime) (*runapp.DispatchSupervisor, error) {
	if control == nil || !c.DispatchEnabled || runtime == nil || runtime.executor == nil {
		return nil, errors.New("worker dispatch requires an explicitly reviewed executor runtime")
	}
	dispatcher, err := runapp.NewDispatcher(control, runtime.executor)
	if err != nil {
		return nil, err
	}
	return runapp.NewDispatchSupervisor(control, dispatcher, heartbeat.New(), runapp.SupervisorConfig{
		WorkerID: c.WorkerID, LeaseTTL: c.LeaseTTL, HeartbeatInterval: c.HeartbeatInterval, ActivationLimit: c.ActivationLimit,
	})
}

func nonFatalDispatchError(err error) bool {
	return errors.Is(err, runapp.ErrNoWork) || errors.Is(err, runapp.ErrExecutorOutcomeUnknown) || errors.Is(err, runapp.ErrInvalidExecutorResponse) || errors.Is(err, runapp.ErrWorkerLeaseLost)
}

func runWorker(ctx context.Context, logger *slog.Logger, getenv func(string) string, services *ReviewedWorkerServices) error {
	c, err := LoadWorkerConfig(getenv)
	if err != nil {
		return err
	}
	if c.DispatchEnabled && (services == nil || services.dispatch == nil) {
		return errors.New("worker dispatch requires an explicitly reviewed executor runtime")
	}
	if !c.ControlEnabled {
		if services != nil && (services.providerControl != nil || services.settlement != nil) {
			return errors.New("reviewed background services require worker control and explicit workspaces")
		}
		logger.Info("worker started", "mode", "idle", "task_processing_enabled", false)
		<-ctx.Done()
		logger.Info("worker stopped gracefully")
		return nil
	}
	control, closeResources, err := BuildWorkerControl(ctx, c)
	if err != nil {
		return err
	}
	defer closeResources()
	var supervisor *runapp.DispatchSupervisor
	if c.DispatchEnabled {
		supervisor, err = BuildDispatchSupervisor(control, c, services.dispatch)
		if err != nil {
			return err
		}
	}
	logger.Info("worker control started", "worker_id", c.WorkerID, "workspace_count", len(c.Workspaces), "lease_dispatch_enabled", c.DispatchEnabled)
	ticker := time.NewTicker(c.PollInterval)
	defer ticker.Stop()
	cycle := func() error {
		for _, workspace := range c.Workspaces {
			count, e := control.RecoverExpired(ctx, workspace, 100)
			if e != nil {
				return e
			}
			if count > 0 {
				logger.Info("expired worker leases recovered", "workspace_id", string(workspace), "count", count)
			}
		}
		if supervisor != nil {
			// The first production-safe policy is deliberately serial: at most one
			// supplier submission is active in this process. Throughput can be raised
			// only after an explicit bounded-concurrency review.
			for _, workspace := range c.Workspaces {
				result, e := supervisor.DispatchOne(ctx, workspace, c.DeploymentRevisions)
				if e == nil {
					logger.Info("supplier submission accepted", "workspace_id", string(workspace), "run_id", string(result.Run.ID), "submission_key", result.SubmissionKey)
					continue
				}
				if nonFatalDispatchError(e) {
					if !errors.Is(e, runapp.ErrNoWork) {
						logger.Warn("supplier submission requires no immediate retry", "workspace_id", string(workspace), "error", e.Error())
					}
					continue
				}
				return e
			}
		}
		if _, e := RunReviewedRuntimeCycle(ctx, logger, c.Workspaces, services); e != nil {
			return e
		}
		return nil
	}
	if err = cycle(); err != nil {
		if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			logger.Info("worker control stopped gracefully")
			return nil
		}
		return err
	}
	for {
		select {
		case <-ctx.Done():
			logger.Info("worker control stopped gracefully")
			return nil
		case <-ticker.C:
			if err = cycle(); err != nil {
				if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
					logger.Info("worker control stopped gracefully")
					return nil
				}
				return err
			}
		}
	}
}

// RunWorker keeps supplier dispatch fail-closed because no production secret
// provider is wired by the default command. A future reviewed composition may
// call RunWorkerWithReviewedRuntime with an explicitly constructed runtime.
func RunWorker(ctx context.Context, logger *slog.Logger) error {
	return runWorker(ctx, logger, os.Getenv, nil)
}

func RunWorkerWithReviewedRuntime(ctx context.Context, logger *slog.Logger, runtime *ReviewedDispatchRuntime) error {
	services, err := NewReviewedWorkerServices(runtime, nil, nil)
	if err != nil {
		return err
	}
	return runWorker(ctx, logger, os.Getenv, services)
}

func RunWorkerWithReviewedServices(ctx context.Context, logger *slog.Logger, services *ReviewedWorkerServices) error {
	if services == nil {
		return errors.New("reviewed worker services are required")
	}
	return runWorker(ctx, logger, os.Getenv, services)
}
