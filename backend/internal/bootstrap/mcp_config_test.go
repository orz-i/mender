package bootstrap

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"
)

func TestMCPGatewayConfigurationRequiresExistingSafeCapabilities(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{17}, 32))
	valid := map[string]string{
		"MENDER_RUN_API_ENABLED":                "true",
		"MENDER_RUN_READ_API_ENABLED":           "true",
		"MENDER_RUN_START_API_ENABLED":          "true",
		"MENDER_RUN_COORDINATED_CANCEL_ENABLED": "true",
		"MENDER_MCP_GATEWAY_ENABLED":            "true",
		"MENDER_DATABASE_URL":                   "postgres://reader-not-connected",
		"MENDER_CURSOR_SIGNING_KEY":             key,
		"MENDER_ADMISSION_DATABASE_URL":         "postgres://admission-not-connected",
		"MENDER_CANCELLATION_DATABASE_URL":      "postgres://cancel-not-connected",
	}
	cfg, err := LoadAPIConfig(func(k string) string { return valid[k] })
	if err != nil || !cfg.MCPGatewayEnabled || !cfg.RunReadAPIEnabled || !cfg.StartRunAPIEnabled || !cfg.CoordinatedCancelEnabled {
		t.Fatal(cfg, err)
	}
	for _, change := range []map[string]string{
		{"MENDER_MCP_GATEWAY_ENABLED": "yes"},
		{"MENDER_RUN_API_ENABLED": "false"},
		{"MENDER_RUN_READ_API_ENABLED": "false"},
		{"MENDER_RUN_START_API_ENABLED": "false"},
		{"MENDER_RUN_COORDINATED_CANCEL_ENABLED": "false"},
	} {
		_, err = LoadAPIConfig(func(k string) string {
			if value, ok := change[k]; ok { return value }
			return valid[k]
		})
		if err == nil { t.Fatal("unsafe MCP configuration accepted", change) }
	}
	if h, _, err := BuildAPI(context.Background(), APIConfig{RunAPIEnabled: true, MCPGatewayEnabled: true}); err == nil || h != nil {
		t.Fatal("directly constructed MCP config bypassed prerequisite checks")
	}
}

