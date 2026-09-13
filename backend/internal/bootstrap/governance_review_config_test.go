package bootstrap

import "testing"

func TestGovernanceReviewConfigRequiresOIDCCatalogAndSeparateRole(t *testing.T) {
	base := map[string]string{
		"MENDER_RUN_API_ENABLED":                  "true",
		"MENDER_DATABASE_URL":                     "postgres://runtime-not-connected",
		"MENDER_CONSOLE_OIDC_ENABLED":             "true",
		"MENDER_BROWSER_SESSION_DATABASE_URL":     "postgres://browser-session-not-connected",
		"MENDER_CONSOLE_OIDC_ISSUER":              "https://idp.example",
		"MENDER_CONSOLE_OIDC_CLIENT_ID":           "mender-console",
		"MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE":  "oidc-client-secret-file",
		"MENDER_CONSOLE_OIDC_REDIRECT_URL":        "https://mender.example/auth/callback",
		"MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE":    "flow-signing-key-file",
		"MENDER_CONSOLE_CATALOG_ENABLED":          "true",
		"MENDER_CATALOG_MANAGER_DATABASE_URL":     "postgres://catalog-manager-not-connected",
		"MENDER_ADMIN_CATALOG_REVIEW_ENABLED":     "true",
		"MENDER_GOVERNANCE_REVIEWER_DATABASE_URL": "postgres://governance-reviewer-not-connected",
	}
	getenv := func(key string) string { return base[key] }
	cfg, err := LoadAPIConfig(getenv)
	if err != nil || !cfg.AdminCatalogReviewEnabled || cfg.GovernanceReviewerDatabaseURL == "" {
		t.Fatal(cfg, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"MENDER_ADMIN_CATALOG_REVIEW_ENABLED", "yes"},
		{"MENDER_CONSOLE_OIDC_ENABLED", "false"},
		{"MENDER_CONSOLE_CATALOG_ENABLED", "false"},
		{"MENDER_GOVERNANCE_REVIEWER_DATABASE_URL", ""},
	} {
		copy := make(map[string]string, len(base))
		for k, v := range base {
			copy[k] = v
		}
		copy[tc.key] = tc.value
		if _, err = LoadAPIConfig(func(key string) string { return copy[key] }); err == nil {
			t.Fatal("invalid Governance review config accepted", tc.key, tc.value)
		}
	}
}
