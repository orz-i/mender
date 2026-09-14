package bootstrap

import "testing"

func TestProviderCallbackConfigurationIsExplicitAndFailsClosed(t *testing.T) {
	valid := map[string]string{
		"MENDER_RUN_API_ENABLED":                "true",
		"MENDER_DATABASE_URL":                   "postgres://runtime-not-connected",
		"MENDER_PROVIDER_CALLBACK_ENABLED":      "true",
		"MENDER_CALLBACK_INGESTOR_DATABASE_URL": "postgres://callback-not-connected",
		"MENDER_PROVIDER_CALLBACK_SECRET_ROOT":  `C:\mounted-callback-secrets`,
		"MENDER_PROVIDER_CALLBACK_KEYS":         "provider_a=key_current|key_previous,provider_b=key_only",
	}
	cfg, err := LoadAPIConfig(func(key string) string { return valid[key] })
	if err != nil || !cfg.ProviderCallbackEnabled || cfg.ProviderCallbackDatabaseURL == "" || cfg.ProviderCallbackSecretRoot == "" || len(cfg.ProviderCallbackReviewedKeys) != 2 || len(cfg.ProviderCallbackReviewedKeys["provider_a"]) != 2 {
		t.Fatal(cfg, err)
	}
	for _, change := range []map[string]string{
		{"MENDER_PROVIDER_CALLBACK_ENABLED": "yes"},
		{"MENDER_RUN_API_ENABLED": "false"},
		{"MENDER_CALLBACK_INGESTOR_DATABASE_URL": ""},
		{"MENDER_PROVIDER_CALLBACK_SECRET_ROOT": ""},
		{"MENDER_PROVIDER_CALLBACK_KEYS": ""},
		{"MENDER_PROVIDER_CALLBACK_KEYS": "provider_a=key_a|key_b|key_c"},
		{"MENDER_PROVIDER_CALLBACK_KEYS": "provider_a=key_a,provider_a=key_b"},
		{"MENDER_PROVIDER_CALLBACK_KEYS": "bad provider=key_a"},
	} {
		_, err = LoadAPIConfig(func(key string) string {
			if value, ok := change[key]; ok {
				return value
			}
			return valid[key]
		})
		if err == nil {
			t.Fatal("invalid Provider Callback configuration accepted", change)
		}
	}
}

func TestAdminProviderCallbackObservabilityRequiresHumanSessionAndObserverRole(t *testing.T) {
	valid := map[string]string{
		"MENDER_RUN_API_ENABLED":                    "true",
		"MENDER_DATABASE_URL":                       "postgres://runtime-not-connected",
		"MENDER_CONSOLE_OIDC_ENABLED":               "true",
		"MENDER_BROWSER_SESSION_DATABASE_URL":        "postgres://session-not-connected",
		"MENDER_CONSOLE_OIDC_ISSUER":                 "https://issuer.example",
		"MENDER_CONSOLE_OIDC_CLIENT_ID":              "console",
		"MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE":     `C:\mounted\oidc.secret`,
		"MENDER_CONSOLE_OIDC_REDIRECT_URL":           "https://console.example/auth/callback",
		"MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE":       `C:\mounted\flow.key`,
		"MENDER_ADMIN_PROVIDER_CALLBACKS_ENABLED":    "true",
		"MENDER_CALLBACK_OBSERVER_DATABASE_URL":      "postgres://callback-observer-not-connected",
	}
	cfg, err := LoadAPIConfig(func(key string) string { return valid[key] })
	if err != nil || !cfg.AdminProviderCallbacksEnabled || cfg.CallbackObserverDatabaseURL == "" {
		t.Fatal(cfg, err)
	}
	for _, change := range []map[string]string{
		{"MENDER_ADMIN_PROVIDER_CALLBACKS_ENABLED": "yes"},
		{"MENDER_CONSOLE_OIDC_ENABLED": "false"},
		{"MENDER_CALLBACK_OBSERVER_DATABASE_URL": ""},
	} {
		_, err = LoadAPIConfig(func(key string) string {
			if value, ok := change[key]; ok { return value }
			return valid[key]
		})
		if err == nil { t.Fatal("invalid Admin Provider Callback configuration accepted", change) }
	}
}
