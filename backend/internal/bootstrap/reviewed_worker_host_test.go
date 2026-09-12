package bootstrap

import (
	"context"
	"strings"
	"testing"
)

func hostEnv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestReviewedWorkerHostConfigIsDisabledByDefaultAndRequiresMasterSwitch(t *testing.T) {
	cfg, err := LoadReviewedWorkerHostConfig(hostEnv(nil))
	if err != nil || cfg.Enabled {
		t.Fatal(cfg, err)
	}
	if _, err = LoadReviewedWorkerHostConfig(hostEnv(map[string]string{"MENDER_REVIEWED_SETTLEMENT_ENABLED": "true"})); err == nil {
		t.Fatal("background capability was accepted without reviewed master switch")
	}
	if _, err = LoadReviewedWorkerHostConfig(hostEnv(map[string]string{"MENDER_REVIEWED_WORKER_RUNTIME_ENABLED": "yes"})); err == nil {
		t.Fatal("non-strict reviewed worker flag was accepted")
	}
}

func TestReviewedWorkerHostConfigRequiresExplicitLocationsAndAllowlists(t *testing.T) {
	base := map[string]string{
		"MENDER_REVIEWED_WORKER_RUNTIME_ENABLED": "true",
		"MENDER_REVIEWED_HTTP_DISPATCH_ENABLED":  "true",
		"MENDER_REVIEWED_SECRET_ROOT":            `C:\mounted-secrets`,
		"MENDER_EXECUTOR_DATABASE_URL":           "postgres://executor:pw@127.0.0.1:5432/mender?sslmode=disable",
		"MENDER_REVIEWED_EGRESS_ALLOWED_HOSTS":   "provider.example",
	}
	cfg, err := LoadReviewedWorkerHostConfig(hostEnv(base))
	if err != nil || !cfg.Enabled || !cfg.HTTPDispatchEnabled || len(cfg.AllowedHosts) != 1 || cfg.AllowHTTP || cfg.AllowLoopback {
		t.Fatal(cfg, err)
	}
	for _, key := range []string{"MENDER_REVIEWED_SECRET_ROOT", "MENDER_EXECUTOR_DATABASE_URL", "MENDER_REVIEWED_EGRESS_ALLOWED_HOSTS"} {
		copy := map[string]string{}
		for k, v := range base {
			copy[k] = v
		}
		delete(copy, key)
		if _, err = LoadReviewedWorkerHostConfig(hostEnv(copy)); err == nil {
			t.Fatal("missing reviewed host input was accepted", key)
		}
	}
	base["MENDER_REVIEWED_EGRESS_ALLOWED_HOSTS"] = "provider.example,provider.example"
	if _, err = LoadReviewedWorkerHostConfig(hostEnv(base)); err == nil {
		t.Fatal("duplicate egress allowlist entry was accepted")
	}
}

func TestReviewedWorkerHostConfigScopesProviderControlAndSettlement(t *testing.T) {
	values := map[string]string{
		"MENDER_REVIEWED_WORKER_RUNTIME_ENABLED":   "true",
		"MENDER_REVIEWED_PROVIDER_CONTROL_ENABLED": "true",
		"MENDER_REVIEWED_SETTLEMENT_ENABLED":       "true",
		"MENDER_REVIEWED_SECRET_ROOT":              `C:\mounted-secrets`,
		"MENDER_EXECUTOR_DATABASE_URL":             "postgres://executor:pw@127.0.0.1:5432/mender?sslmode=disable",
		"MENDER_RECONCILER_DATABASE_URL":           "postgres://reconciler:pw@127.0.0.1:5432/mender?sslmode=disable",
		"MENDER_SETTLEMENT_DATABASE_URL":           "postgres://settlement:pw@127.0.0.1:5432/mender?sslmode=disable",
		"MENDER_REVIEWED_PROVIDER_IDS":             "provider_a,provider_b",
		"MENDER_REVIEWED_EGRESS_ALLOWED_HOSTS":     "one.example,two.example",
		"MENDER_REVIEWED_EGRESS_ALLOW_HTTP":        "false",
		"MENDER_REVIEWED_EGRESS_ALLOW_LOOPBACK":    "false",
	}
	cfg, err := LoadReviewedWorkerHostConfig(hostEnv(values))
	if err != nil || !cfg.ProviderControlEnabled || !cfg.SettlementEnabled || len(cfg.ProviderIDs) != 2 || len(cfg.AllowedHosts) != 2 {
		t.Fatal(cfg, err)
	}
	delete(values, "MENDER_RECONCILER_DATABASE_URL")
	if _, err = LoadReviewedWorkerHostConfig(hostEnv(values)); err == nil || !strings.Contains(err.Error(), "reconciler") {
		t.Fatal("provider control without reconciler principal did not fail closed", err)
	}
}

func TestReviewedWorkerServicesRejectsDispatchMismatchBeforeOpeningResources(t *testing.T) {
	worker := WorkerConfig{ControlEnabled: true, DatabaseURL: "postgres://not-opened", WorkerID: "worker_a"}
	cfg := ReviewedWorkerHostConfig{Enabled: true, HTTPDispatchEnabled: true, SecretRoot: t.TempDir(), ExecutorDatabaseURL: "postgres://not-opened", AllowedHosts: []string{"provider.example"}}
	if services, closeIt, err := BuildReviewedWorkerServicesFromConfig(context.Background(), worker, cfg); err == nil || services != nil || closeIt != nil || !strings.Contains(err.Error(), "enabled together") {
		t.Fatal("dispatch mismatch reached resource composition", services != nil, closeIt != nil, err)
	}
}
