package bootstrap

import (
	"context"
	"io"
	"log/slog"
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
	for _, change := range []map[string]string{
		{"MENDER_WORKER_DISPATCH_ENABLED": "true"},
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
	if control, closeIt, err := BuildWorkerControl(context.Background(), WorkerConfig{}); err == nil || control != nil || closeIt != nil {
		t.Fatal("disabled worker unexpectedly built control plane")
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
