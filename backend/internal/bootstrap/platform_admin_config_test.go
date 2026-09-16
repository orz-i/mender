package bootstrap

import "testing"

func TestPlatformAdminConfigRequiresOIDCAndSeparateRole(t *testing.T) {
	base := map[string]string{
		"MENDER_RUN_API_ENABLED":                     "true",
		"MENDER_DATABASE_URL":                        "postgres://runtime-not-connected",
		"MENDER_CONSOLE_OIDC_ENABLED":                "true",
		"MENDER_BROWSER_SESSION_DATABASE_URL":        "postgres://browser-session-not-connected",
		"MENDER_CONSOLE_OIDC_ISSUER":                 "https://idp.example",
		"MENDER_CONSOLE_OIDC_CLIENT_ID":              "mender-console",
		"MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE":     "oidc-client-secret-file",
		"MENDER_CONSOLE_OIDC_REDIRECT_URL":           "https://mender.example/auth/callback",
		"MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE":       "flow-signing-key-file",
		"MENDER_ADMIN_PLATFORM_OPERATIONS_ENABLED":   "true",
		"MENDER_PLATFORM_ADMIN_MANAGER_DATABASE_URL": "postgres://platform-admin-manager-not-connected",
	}
	cfg, err := LoadAPIConfig(func(key string) string { return base[key] })
	if err != nil || !cfg.AdminPlatformOperationsEnabled || cfg.PlatformAdminManagerDatabaseURL == "" {
		t.Fatal(cfg, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"MENDER_ADMIN_PLATFORM_OPERATIONS_ENABLED", "yes"},
		{"MENDER_CONSOLE_OIDC_ENABLED", "false"},
		{"MENDER_PLATFORM_ADMIN_MANAGER_DATABASE_URL", ""},
	} {
		copy := make(map[string]string, len(base))
		for k, v := range base {
			copy[k] = v
		}
		copy[tc.key] = tc.value
		if _, err = LoadAPIConfig(func(key string) string { return copy[key] }); err == nil {
			t.Fatal("invalid Platform Admin config accepted", tc.key, tc.value)
		}
	}
}
