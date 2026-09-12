package bootstrap

import (
	"bytes"
	"encoding/base64"
	"testing"
	"time"
)

func TestConsoleRunDelegationRequiresOIDCAndRunReadAndBoundsTTL(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	valid := map[string]string{
		"MENDER_RUN_API_ENABLED":                    "true",
		"MENDER_RUN_READ_API_ENABLED":               "true",
		"MENDER_DATABASE_URL":                       "postgres://runtime-not-connected",
		"MENDER_CURSOR_SIGNING_KEY":                 key,
		"MENDER_CONSOLE_OIDC_ENABLED":               "true",
		"MENDER_BROWSER_SESSION_DATABASE_URL":        "postgres://browser-not-connected",
		"MENDER_CONSOLE_OIDC_ISSUER":                 "https://idp.example",
		"MENDER_CONSOLE_OIDC_CLIENT_ID":              "mender-console",
		"MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE":     "client-secret-file",
		"MENDER_CONSOLE_OIDC_REDIRECT_URL":           "https://mender.example/auth/callback",
		"MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE":       "flow-key-file",
		"MENDER_CONSOLE_RUN_DELEGATION_ENABLED":      "true",
		"MENDER_CONSOLE_RUN_DELEGATION_TTL":          "7m",
	}
	cfg, err := LoadAPIConfig(func(name string) string { return valid[name] })
	if err != nil || !cfg.ConsoleRunDelegationEnabled || cfg.ConsoleRunDelegationTTL != 7*time.Minute {
		t.Fatal(cfg, err)
	}
	for _, change := range []map[string]string{
		{"MENDER_CONSOLE_OIDC_ENABLED": "false"},
		{"MENDER_RUN_READ_API_ENABLED": "false"},
		{"MENDER_CONSOLE_RUN_DELEGATION_ENABLED": "yes"},
		{"MENDER_CONSOLE_RUN_DELEGATION_TTL": "30s"},
		{"MENDER_CONSOLE_RUN_DELEGATION_TTL": "31m"},
	} {
		_, err = LoadAPIConfig(func(name string) string {
			if value, ok := change[name]; ok {
				return value
			}
			return valid[name]
		})
		if err == nil {
			t.Fatal("invalid Console Run delegation configuration accepted", change)
		}
	}
}
