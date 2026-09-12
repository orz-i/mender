package bootstrap

import "testing"

func TestConsoleHumanStartRequiresOIDCDiscoveryAndAdmission(t *testing.T) {
	valid := map[string]string{
		"MENDER_RUN_API_ENABLED":                    "true",
		"MENDER_DATABASE_URL":                       "postgres://runtime-not-connected",
		"MENDER_RUN_START_API_ENABLED":              "true",
		"MENDER_ADMISSION_DATABASE_URL":             "postgres://admission-not-connected",
		"MENDER_CONSOLE_OIDC_ENABLED":               "true",
		"MENDER_BROWSER_SESSION_DATABASE_URL":       "postgres://browser-not-connected",
		"MENDER_CONSOLE_OIDC_ISSUER":                "https://idp.example",
		"MENDER_CONSOLE_OIDC_CLIENT_ID":             "mender-console",
		"MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE":    "oidc-client-secret-file",
		"MENDER_CONSOLE_OIDC_REDIRECT_URL":          "https://mender.example/auth/callback",
		"MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE":      "flow-signing-key-file",
		"MENDER_CONSOLE_LAUNCH_DISCOVERY_ENABLED":   "true",
		"MENDER_CONSOLE_HUMAN_START_ENABLED":        "true",
		"MENDER_CONSOLE_HUMAN_START_DELEGATION_TTL": "4m",
	}
	cfg, err := LoadAPIConfig(func(name string) string { return valid[name] })
	if err != nil || !cfg.ConsoleHumanStartEnabled || cfg.ConsoleHumanStartDelegationTTL != 4*60*1e9 {
		t.Fatal(cfg, err)
	}
	for _, change := range []map[string]string{
		{"MENDER_CONSOLE_HUMAN_START_ENABLED": "yes"},
		{"MENDER_CONSOLE_OIDC_ENABLED": "false"},
		{"MENDER_CONSOLE_LAUNCH_DISCOVERY_ENABLED": "false"},
		{"MENDER_RUN_START_API_ENABLED": "false"},
		{"MENDER_CONSOLE_HUMAN_START_DELEGATION_TTL": "30s"},
		{"MENDER_CONSOLE_HUMAN_START_DELEGATION_TTL": "11m"},
	} {
		_, err = LoadAPIConfig(func(name string) string {
			if value, ok := change[name]; ok {
				return value
			}
			return valid[name]
		})
		if err == nil {
			t.Fatal("unsafe Human StartRun configuration accepted", change)
		}
	}
}
