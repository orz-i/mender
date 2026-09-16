package bootstrap

import "testing"

func TestSupportAccessConfigRequiresDangerousGovernanceAndSeparateReader(t *testing.T) {
	base := map[string]string{
		"MENDER_RUN_API_ENABLED":                          "true",
		"MENDER_DATABASE_URL":                             "postgres://runtime-not-connected",
		"MENDER_CONSOLE_OIDC_ENABLED":                     "true",
		"MENDER_BROWSER_SESSION_DATABASE_URL":             "postgres://browser-session-not-connected",
		"MENDER_CONSOLE_OIDC_ISSUER":                      "https://idp.example",
		"MENDER_CONSOLE_OIDC_CLIENT_ID":                   "mender-console",
		"MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE":          "oidc-client-secret-file",
		"MENDER_CONSOLE_OIDC_REDIRECT_URL":                "https://mender.example/auth/callback",
		"MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE":            "flow-signing-key-file",
		"MENDER_ADMIN_DANGEROUS_OPERATION_ENABLED":        "true",
		"MENDER_DANGEROUS_OPERATION_MANAGER_DATABASE_URL": "postgres://dangerous-operation-manager-not-connected",
		"MENDER_ADMIN_SUPPORT_ACCESS_ENABLED":             "true",
		"MENDER_SUPPORT_READER_DATABASE_URL":              "postgres://support-reader-not-connected",
	}
	cfg, err := LoadAPIConfig(func(key string) string { return base[key] })
	if err != nil || !cfg.AdminSupportAccessEnabled || cfg.SupportReaderDatabaseURL == "" {
		t.Fatal(cfg, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"MENDER_ADMIN_SUPPORT_ACCESS_ENABLED", "yes"},
		{"MENDER_ADMIN_DANGEROUS_OPERATION_ENABLED", "false"},
		{"MENDER_SUPPORT_READER_DATABASE_URL", ""},
	} {
		copy := make(map[string]string, len(base))
		for k, v := range base {
			copy[k] = v
		}
		copy[tc.key] = tc.value
		if _, err = LoadAPIConfig(func(key string) string { return copy[key] }); err == nil {
			t.Fatal("invalid Support Access config accepted", tc.key, tc.value)
		}
	}
}
