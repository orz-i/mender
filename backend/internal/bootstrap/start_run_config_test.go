package bootstrap

import (
	"context"
	"testing"
)

func TestStartRunFeatureIsExplicitAndRequiresSeparateWriter(t *testing.T) {
	valid := map[string]string{
		"MENDER_RUN_API_ENABLED":        "true",
		"MENDER_RUN_START_API_ENABLED":  "true",
		"MENDER_DATABASE_URL":           "postgres://reader-not-connected",
		"MENDER_ADMISSION_DATABASE_URL": "postgres://writer-not-connected",
	}
	cfg, err := LoadAPIConfig(func(k string) string { return valid[k] })
	if err != nil || !cfg.StartRunAPIEnabled || cfg.AdmissionDatabaseURL == "" {
		t.Fatal(cfg, err)
	}
	for _, change := range []map[string]string{
		{"MENDER_RUN_API_ENABLED": "false"},
		{"MENDER_RUN_START_API_ENABLED": "yes"},
		{"MENDER_ADMISSION_DATABASE_URL": ""},
	} {
		_, err = LoadAPIConfig(func(k string) string {
			if v, ok := change[k]; ok {
				return v
			}
			return valid[k]
		})
		if err == nil {
			t.Fatal("unsafe StartRun configuration accepted", change)
		}
	}
	if h, _, err := BuildAPI(context.Background(), APIConfig{StartRunAPIEnabled: true}); err == nil || h != nil {
		t.Fatal("StartRun silently enabled without authenticated Run API")
	}
}
