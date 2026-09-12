package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestWorkerControlIsDisabledByDefaultAndFailsClosed(t *testing.T) {
	cfg, err := LoadWorkerConfig(func(string) string { return "" })
	if err != nil || cfg.ControlEnabled {
		t.Fatal(cfg, err)
	}
	valid := map[string]string{
		"MENDER_WORKER_CONTROL_ENABLED": "true",
		"MENDER_WORKER_DATABASE_URL":    "postgres://not-connected",
		"MENDER_WORKER_ID":              "worker_a",
		"MENDER_WORKER_WORKSPACES":      "ws_a,ws_b",
	}
	cfg, err = LoadWorkerConfig(func(key string) string { return valid[key] })
	if err != nil || !cfg.ControlEnabled || len(cfg.Workspaces) != 2 {
		t.Fatal(cfg, err)
	}
	validDispatch := map[string]string{
		"MENDER_WORKER_CONTROL_ENABLED":      "true",
		"MENDER_WORKER_DISPATCH_ENABLED":     "true",
		"MENDER_WORKER_DATABASE_URL":         "postgres://not-connected",
		"MENDER_WORKER_ID":                   "worker_a",
		"MENDER_WORKER_WORKSPACES":           "ws_a,ws_b",
		"MENDER_WORKER_DEPLOYMENT_REVISIONS": "deploy_a,deploy_b",
		"MENDER_WORKER_LEASE_TTL":            "30s",
		"MENDER_WORKER_HEARTBEAT_INTERVAL":   "10s",
		"MENDER_WORKER_ACTIVATION_LIMIT":     "5",
	}
	cfg, err = LoadWorkerConfig(func(key string) string { return validDispatch[key] })
	if err != nil || !cfg.DispatchEnabled || len(cfg.DeploymentRevisions) != 2 || cfg.ActivationLimit != 5 {
		t.Fatal(cfg, err)
	}
	for _, change := range []map[string]string{
		{"MENDER_WORKER_DISPATCH_ENABLED": "yes"},
		{"MENDER_WORKER_CONTROL_ENABLED": "yes"},
		{"MENDER_WORKER_DATABASE_URL": ""},
		{"MENDER_WORKER_ID": "bad worker"},
		{"MENDER_WORKER_WORKSPACES": ""},
		{"MENDER_WORKER_WORKSPACES": "ws_a,ws_a"},
		{"MENDER_WORKER_POLL_INTERVAL": "10ms"},
	} {
		_, err = LoadWorkerConfig(func(key string) string {
			if value, ok := change[key]; ok {
				return value
			}
			return valid[key]
		})
		if err == nil {
			t.Fatal("unsafe worker configuration accepted", change)
		}
	}
	for _, change := range []map[string]string{
		{"MENDER_WORKER_CONTROL_ENABLED": "false"},
		{"MENDER_WORKER_DEPLOYMENT_REVISIONS": ""},
		{"MENDER_WORKER_DEPLOYMENT_REVISIONS": "deploy_a,deploy_a"},
		{"MENDER_WORKER_LEASE_TTL": "4s"},
		{"MENDER_WORKER_HEARTBEAT_INTERVAL": "15s"},
		{"MENDER_WORKER_HEARTBEAT_INTERVAL": "50ms"},
		{"MENDER_WORKER_ACTIVATION_LIMIT": "0"},
		{"MENDER_WORKER_ACTIVATION_LIMIT": "101"},
	} {
		_, err = LoadWorkerConfig(func(key string) string {
			if value, ok := change[key]; ok {
				return value
			}
			return validDispatch[key]
		})
		if err == nil {
			t.Fatal("unsafe dispatch configuration accepted", change)
		}
	}
	if control, closeIt, err := BuildWorkerControl(context.Background(), WorkerConfig{}); err == nil || control != nil || closeIt != nil {
		t.Fatal("disabled worker unexpectedly built control plane")
	}
	if runtime, err := NewReviewedDispatchRuntime(nil); err == nil || runtime != nil {
		t.Fatal("nil executor became a reviewed dispatch runtime")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err = runWorker(context.Background(), logger, func(key string) string { return validDispatch[key] }, nil)
	if err == nil || !strings.Contains(err.Error(), "explicitly reviewed executor runtime") {
		t.Fatal("dispatch without reviewed runtime did not fail before database access", err)
	}
	background, err := NewReviewedWorkerServices(nil, nil, &ReviewedUsageSettlementRuntime{service: &fakeSettlementRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	err = runWorker(context.Background(), logger, func(string) string { return "" }, background)
	if err == nil || !strings.Contains(err.Error(), "require worker control") {
		t.Fatal("background runtime without explicit worker scope did not fail closed", err)
	}
}

func TestDisabledWorkerStopsWithoutDatabase(t *testing.T) {
	t.Setenv("MENDER_WORKER_CONTROL_ENABLED", "false")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RunWorker(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
}
