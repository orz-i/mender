package bootstrap

import "testing"

func TestPluginPublicationConfigRequiresOIDCAndSeparateRoles(t *testing.T) {
	base := map[string]string{
		"MENDER_RUN_API_ENABLED":                  "true",
		"MENDER_DATABASE_URL":                     "postgres://runtime-not-connected",
		"MENDER_CONSOLE_OIDC_ENABLED":             "true",
		"MENDER_BROWSER_SESSION_DATABASE_URL":     "postgres://browser-session-not-connected",
		"MENDER_CONSOLE_OIDC_ISSUER":              "https://issuer.example.test",
		"MENDER_CONSOLE_OIDC_CLIENT_ID":           "mender-console",
		"MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE":  "testdata/not-read-by-config",
		"MENDER_CONSOLE_OIDC_REDIRECT_URL":        "https://console.example.test/api/console/v1/auth/callback",
		"MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE":    "testdata/not-read-by-config",
		"MENDER_CONSOLE_PUBLISHER_ENABLED":        "true",
		"MENDER_PUBLISHER_MANAGER_DATABASE_URL":   "postgres://publisher-manager-not-connected",
		"MENDER_ADMIN_PLUGIN_REVIEW_ENABLED":      "true",
		"MENDER_GOVERNANCE_REVIEWER_DATABASE_URL": "postgres://governance-reviewer-not-connected",
	}
	getenv := func(key string) string { return base[key] }
	cfg, err := LoadAPIConfig(getenv)
	if err != nil || !cfg.ConsolePublisherEnabled || !cfg.AdminPluginReviewEnabled || cfg.PublisherManagerDatabaseURL == "" || cfg.GovernanceReviewerDatabaseURL == "" {
		t.Fatalf("unexpected Plugin publication config: %#v err=%v", cfg, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"MENDER_CONSOLE_PUBLISHER_ENABLED", "yes"},
		{"MENDER_CONSOLE_OIDC_ENABLED", "false"},
		{"MENDER_PUBLISHER_MANAGER_DATABASE_URL", ""},
		{"MENDER_ADMIN_PLUGIN_REVIEW_ENABLED", "yes"},
		{"MENDER_GOVERNANCE_REVIEWER_DATABASE_URL", ""},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			copy := make(map[string]string, len(base))
			for key, value := range base {
				copy[key] = value
			}
			copy[tc.key] = tc.value
			if _, err := LoadAPIConfig(func(key string) string { return copy[key] }); err == nil {
				t.Fatal("expected fail-closed config rejection")
			}
		})
	}
}
