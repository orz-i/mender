package bootstrap

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestFixedToolsetMCPConfigurationIsExplicitAndDependsOnGateway(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{19}, 32))
	valid := map[string]string{
		"MENDER_RUN_API_ENABLED":                "true",
		"MENDER_RUN_READ_API_ENABLED":           "true",
		"MENDER_RUN_START_API_ENABLED":          "true",
		"MENDER_RUN_COORDINATED_CANCEL_ENABLED": "true",
		"MENDER_MCP_GATEWAY_ENABLED":            "true",
		"MENDER_MCP_FIXED_TOOLSET_ENABLED":      "true",
		"MENDER_DATABASE_URL":                   "postgres://reader-not-connected",
		"MENDER_CURSOR_SIGNING_KEY":             key,
		"MENDER_ADMISSION_DATABASE_URL":         "postgres://admission-not-connected",
		"MENDER_CANCELLATION_DATABASE_URL":      "postgres://cancel-not-connected",
	}
	cfg, err := LoadAPIConfig(func(name string) string { return valid[name] })
	if err != nil || !cfg.MCPGatewayEnabled || !cfg.MCPFixedToolsetEnabled {
		t.Fatal(cfg, err)
	}
	for _, change := range []map[string]string{
		{"MENDER_MCP_FIXED_TOOLSET_ENABLED": "yes"},
		{"MENDER_MCP_GATEWAY_ENABLED": "false"},
	} {
		_, err = LoadAPIConfig(func(name string) string {
			if value, ok := change[name]; ok {
				return value
			}
			return valid[name]
		})
		if err == nil {
			t.Fatal("unsafe Fixed Toolset MCP configuration accepted", change)
		}
	}
}
