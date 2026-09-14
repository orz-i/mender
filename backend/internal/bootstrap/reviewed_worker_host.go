package bootstrap

import (
	"context"
	"errors"
	"strings"
	"sync"

	filesecret "github.com/orz-i/mender/backend/internal/contexts/supply/adapters/outbound/filesecret"
	supplydomain "github.com/orz-i/mender/backend/internal/contexts/supply/domain"
)

type ReviewedWorkerHostConfig struct {
	Enabled                bool
	HTTPDispatchEnabled    bool
	AgentDispatchEnabled   bool
	ProviderControlEnabled bool
	SettlementEnabled      bool
	SecretRoot             string
	ExecutorDatabaseURL    string
	ReconcilerDatabaseURL  string
	SettlementDatabaseURL  string
	ProviderIDs            []string
	AllowedHosts           []string
	AllowHTTP              bool
	AllowLoopback          bool
}

func strictBool(getenv func(string) string, key string) (bool, error) {
	switch strings.TrimSpace(getenv(key)) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New(key + " must be true or false")
	}
}

func reviewedList(raw string, max int) ([]string, error) {
	parts := strings.Split(raw, ",")
	if len(parts) < 1 || len(parts) > max {
		return nil, errors.New("reviewed list is empty or too large")
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" || seen[value] {
			return nil, errors.New("reviewed list contains empty or duplicate value")
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, nil
}

func LoadReviewedWorkerHostConfig(getenv func(string) string) (ReviewedWorkerHostConfig, error) {
	var c ReviewedWorkerHostConfig
	var err error
	if c.Enabled, err = strictBool(getenv, "MENDER_REVIEWED_WORKER_RUNTIME_ENABLED"); err != nil {
		return ReviewedWorkerHostConfig{}, err
	}
	if c.HTTPDispatchEnabled, err = strictBool(getenv, "MENDER_REVIEWED_HTTP_DISPATCH_ENABLED"); err != nil {
		return ReviewedWorkerHostConfig{}, err
	}
	if c.AgentDispatchEnabled, err = strictBool(getenv, "MENDER_REVIEWED_AGENT_DISPATCH_ENABLED"); err != nil {
		return ReviewedWorkerHostConfig{}, err
	}
	if c.ProviderControlEnabled, err = strictBool(getenv, "MENDER_REVIEWED_PROVIDER_CONTROL_ENABLED"); err != nil {
		return ReviewedWorkerHostConfig{}, err
	}
	if c.SettlementEnabled, err = strictBool(getenv, "MENDER_REVIEWED_SETTLEMENT_ENABLED"); err != nil {
		return ReviewedWorkerHostConfig{}, err
	}
	if c.AllowHTTP, err = strictBool(getenv, "MENDER_REVIEWED_EGRESS_ALLOW_HTTP"); err != nil {
		return ReviewedWorkerHostConfig{}, err
	}
	if c.AllowLoopback, err = strictBool(getenv, "MENDER_REVIEWED_EGRESS_ALLOW_LOOPBACK"); err != nil {
		return ReviewedWorkerHostConfig{}, err
	}
	anyService := c.HTTPDispatchEnabled || c.AgentDispatchEnabled || c.ProviderControlEnabled || c.SettlementEnabled
	if !c.Enabled {
		if anyService || c.AllowHTTP || c.AllowLoopback {
			return ReviewedWorkerHostConfig{}, errors.New("reviewed worker capabilities require MENDER_REVIEWED_WORKER_RUNTIME_ENABLED=true")
		}
		return c, nil
	}
	if !anyService {
		return ReviewedWorkerHostConfig{}, errors.New("reviewed worker runtime requires at least one capability")
	}
	if c.AgentDispatchEnabled && !c.ProviderControlEnabled {
		return ReviewedWorkerHostConfig{}, errors.New("remote Agent dispatch requires reviewed provider control")
	}
	if c.HTTPDispatchEnabled || c.AgentDispatchEnabled || c.ProviderControlEnabled {
		c.SecretRoot = strings.TrimSpace(getenv("MENDER_REVIEWED_SECRET_ROOT"))
		c.ExecutorDatabaseURL = strings.TrimSpace(getenv("MENDER_EXECUTOR_DATABASE_URL"))
		if c.SecretRoot == "" || c.ExecutorDatabaseURL == "" {
			return ReviewedWorkerHostConfig{}, errors.New("reviewed supplier runtime requires secret root and executor database URL")
		}
		c.AllowedHosts, err = reviewedList(getenv("MENDER_REVIEWED_EGRESS_ALLOWED_HOSTS"), 256)
		if err != nil {
			return ReviewedWorkerHostConfig{}, errors.New("reviewed supplier runtime requires an exact egress host allowlist")
		}
	}
	if c.ProviderControlEnabled {
		c.ReconcilerDatabaseURL = strings.TrimSpace(getenv("MENDER_RECONCILER_DATABASE_URL"))
		if c.ReconcilerDatabaseURL == "" {
			return ReviewedWorkerHostConfig{}, errors.New("provider control requires reconciler database URL")
		}
		c.ProviderIDs, err = reviewedList(getenv("MENDER_REVIEWED_PROVIDER_IDS"), 256)
		if err != nil {
			return ReviewedWorkerHostConfig{}, errors.New("provider control requires reviewed provider IDs")
		}
	}
	if c.SettlementEnabled {
		c.SettlementDatabaseURL = strings.TrimSpace(getenv("MENDER_SETTLEMENT_DATABASE_URL"))
		if c.SettlementDatabaseURL == "" {
			return ReviewedWorkerHostConfig{}, errors.New("usage settlement requires settlement database URL")
		}
	}
	return c, nil
}

// BuildReviewedWorkerServicesFromConfig is the explicit ambient-host
// composition path. Configuration contains locations and allowlists, never raw
// supplier credentials. Secret bytes come from the mounted SecretProvider.
func BuildReviewedWorkerServicesFromConfig(ctx context.Context, worker WorkerConfig, c ReviewedWorkerHostConfig) (*ReviewedWorkerServices, func(), error) {
	if !c.Enabled || !worker.ControlEnabled {
		return nil, nil, errors.New("reviewed worker runtime requires explicit worker control")
	}
	dispatchEnabled := c.HTTPDispatchEnabled || c.AgentDispatchEnabled
	if worker.DispatchEnabled != dispatchEnabled {
		return nil, nil, errors.New("worker dispatch and reviewed supplier dispatch must be enabled together")
	}
	if c.AgentDispatchEnabled && !c.ProviderControlEnabled {
		return nil, nil, errors.New("remote Agent dispatch requires reviewed provider control")
	}
	var closers []func()
	closeAll := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
	fail := func(err error) (*ReviewedWorkerServices, func(), error) {
		closeAll()
		return nil, nil, err
	}

	var secrets *filesecret.Provider
	var err error
	if dispatchEnabled || c.ProviderControlEnabled {
		secrets, err = filesecret.New(c.SecretRoot)
		if err != nil {
			return nil, nil, err
		}
	}

	var dispatch *ReviewedDispatchRuntime
	if dispatchEnabled {
		transports := make([]string, 0, 2)
		if c.HTTPDispatchEnabled {
			transports = append(transports, supplydomain.TransportHTTP)
		}
		if c.AgentDispatchEnabled {
			transports = append(transports, supplydomain.TransportAgentHTTP)
		}
		executor, closeExecutor, err := BuildSupplierHTTPExecutor(ctx, SupplierHTTPRuntimeConfig{
			WorkerDatabaseURL: worker.DatabaseURL, ExecutorDatabaseURL: c.ExecutorDatabaseURL,
			AllowedHosts: c.AllowedHosts, TransportKinds: transports, AllowHTTP: c.AllowHTTP, AllowLoopback: c.AllowLoopback,
		}, secrets)
		if err != nil {
			return fail(err)
		}
		closers = append(closers, closeExecutor)
		dispatch, err = NewReviewedDispatchRuntime(executor)
		if err != nil {
			return fail(err)
		}
	}

	var control *ReviewedProviderControlRuntime
	if c.ProviderControlEnabled {
		controlTransports := []string{supplydomain.TransportHTTP}
		if c.AgentDispatchEnabled {
			controlTransports = append(controlTransports, supplydomain.TransportAgentHTTP)
		}
		var closeControl func()
		control, closeControl, err = BuildProviderHTTPControlRuntime(ctx, ProviderHTTPControlRuntimeConfig{
			ExecutorDatabaseURL: c.ExecutorDatabaseURL, ReconcilerDatabaseURL: c.ReconcilerDatabaseURL,
			ProviderIDs: c.ProviderIDs, AllowedHosts: c.AllowedHosts, TransportKinds: controlTransports, AllowHTTP: c.AllowHTTP, AllowLoopback: c.AllowLoopback,
		}, secrets)
		if err != nil {
			return fail(err)
		}
		closers = append(closers, closeControl)
	}

	var settlement *ReviewedUsageSettlementRuntime
	if c.SettlementEnabled {
		var closeSettlement func()
		settlement, closeSettlement, err = BuildUsageSettlementRuntime(ctx, UsageSettlementRuntimeConfig{DatabaseURL: c.SettlementDatabaseURL})
		if err != nil {
			return fail(err)
		}
		closers = append(closers, closeSettlement)
	}
	services, err := NewReviewedWorkerServices(dispatch, control, settlement)
	if err != nil {
		return fail(err)
	}
	var once sync.Once
	return services, func() { once.Do(closeAll) }, nil
}
