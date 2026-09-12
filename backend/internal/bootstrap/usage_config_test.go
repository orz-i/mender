package bootstrap

import "testing"

func TestConsoleUsageRequiresOIDCAndSeparateObserverRole(t *testing.T) {
	valid := map[string]string{
		"MENDER_RUN_API_ENABLED":                 "true",
		"MENDER_DATABASE_URL":                    "postgres://runtime-not-connected",
		"MENDER_CONSOLE_OIDC_ENABLED":            "true",
		"MENDER_BROWSER_SESSION_DATABASE_URL":    "postgres://browser-not-connected",
		"MENDER_CONSOLE_OIDC_ISSUER":             "https://idp.example",
		"MENDER_CONSOLE_OIDC_CLIENT_ID":          "mender-console",
		"MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE": "oidc-client-secret-file",
		"MENDER_CONSOLE_OIDC_REDIRECT_URL":       "https://mender.example/auth/callback",
		"MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE":   "flow-signing-key-file",
		"MENDER_CONSOLE_USAGE_ENABLED":           "true",
		"MENDER_COMMERCE_OBSERVER_DATABASE_URL":  "postgres://observer-not-connected",
	}
	cfg, err := LoadAPIConfig(func(name string) string { return valid[name] })
	if err != nil || !cfg.ConsoleUsageEnabled || cfg.CommerceObserverDatabaseURL == "" {
		t.Fatal(cfg, err)
	}
	for _, change := range []map[string]string{
		{"MENDER_CONSOLE_USAGE_ENABLED": "yes"},
		{"MENDER_CONSOLE_OIDC_ENABLED": "false"},
		{"MENDER_COMMERCE_OBSERVER_DATABASE_URL": ""},
	} {
		_, err = LoadAPIConfig(func(name string) string {
			if value, ok := change[name]; ok {
				return value
			}
			return valid[name]
		})
		if err == nil {
			t.Fatal("unsafe Console Usage configuration accepted", change)
		}
	}
}
