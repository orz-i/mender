package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	runpg "github.com/orz-i/mender/backend/internal/contexts/execution/adapters/outbound/postgres"
	runapp "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	rundomain "github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

type WorkerConfig struct {
	ControlEnabled bool
	DatabaseURL    string
	WorkerID       string
	Workspaces     []rundomain.WorkspaceID
	PollInterval   time.Duration
}

func LoadWorkerConfig(getenv func(string) string) (WorkerConfig, error) {
	c := WorkerConfig{PollInterval: time.Second}
	switch getenv("MENDER_WORKER_CONTROL_ENABLED") {
	case "", "false":
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

// RunWorker currently runs only the local lease-recovery control plane. It never
// acquires a fresh job or contacts a supplier until an executor dispatcher is added.
func RunWorker(ctx context.Context, logger *slog.Logger) error {
	c, err := LoadWorkerConfig(os.Getenv)
	if err != nil {
		return err
	}
	if !c.ControlEnabled {
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
	logger.Info("worker control started", "worker_id", c.WorkerID, "workspace_count", len(c.Workspaces), "lease_dispatch_enabled", false)
	ticker := time.NewTicker(c.PollInterval)
	defer ticker.Stop()
	recover := func() error {
		for _, workspace := range c.Workspaces {
			count, e := control.RecoverExpired(ctx, workspace, 100)
			if e != nil {
				return e
			}
			if count > 0 {
				logger.Info("expired worker leases recovered", "workspace_id", string(workspace), "count", count)
			}
		}
		return nil
	}
	if err = recover(); err != nil {
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
			if err = recover(); err != nil {
				if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
					logger.Info("worker control stopped gracefully")
					return nil
				}
				return err
			}
		}
	}
}
