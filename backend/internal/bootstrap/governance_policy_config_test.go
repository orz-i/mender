package bootstrap

import "testing"

func TestGovernancePolicyConfigRequiresOIDCCatalogAndSeparateRole(t *testing.T) {
	base := map[string]string{
		"MENDER_RUN_API_ENABLED": "true", "MENDER_DATABASE_URL": "postgres://runtime-not-connected",
		"MENDER_CONSOLE_OIDC_ENABLED": "true", "MENDER_BROWSER_SESSION_DATABASE_URL": "postgres://browser-not-connected",
		"MENDER_CONSOLE_OIDC_ISSUER": "https://idp.example", "MENDER_CONSOLE_OIDC_CLIENT_ID": "mender-console",
		"MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE": "oidc-secret", "MENDER_CONSOLE_OIDC_REDIRECT_URL": "https://mender.example/auth/callback",
		"MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE": "flow-key", "MENDER_CONSOLE_CATALOG_ENABLED": "true",
		"MENDER_CATALOG_MANAGER_DATABASE_URL": "postgres://catalog-not-connected", "MENDER_ADMIN_CATALOG_POLICY_ENABLED": "true",
		"MENDER_GOVERNANCE_POLICY_MANAGER_DATABASE_URL": "postgres://policy-not-connected",
	}
	cfg, err := LoadAPIConfig(func(key string) string { return base[key] })
	if err != nil || !cfg.AdminCatalogPolicyEnabled || cfg.GovernancePolicyManagerDatabaseURL == "" {
		t.Fatal(cfg, err)
	}
	for _, key := range []string{"MENDER_GOVERNANCE_POLICY_MANAGER_DATABASE_URL", "MENDER_CONSOLE_OIDC_ENABLED", "MENDER_CONSOLE_CATALOG_ENABLED"} {
		copy := make(map[string]string, len(base))
		for k, v := range base {
			copy[k] = v
		}
		copy[key] = ""
		if key != "MENDER_GOVERNANCE_POLICY_MANAGER_DATABASE_URL" {
			copy[key] = "false"
		}
		if _, err := LoadAPIConfig(func(k string) string { return copy[k] }); err == nil {
			t.Fatal("invalid policy config accepted", key)
		}
	}
}
