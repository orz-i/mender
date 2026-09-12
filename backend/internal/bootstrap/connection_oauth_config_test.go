package bootstrap

import "testing"

func TestConnectionOAuthConfigurationIsExplicitAndFailsClosed(t *testing.T) {
	valid := map[string]string{
		"MENDER_RUN_API_ENABLED":                         "true",
		"MENDER_DATABASE_URL":                            "postgres://runtime-not-connected",
		"MENDER_CONSOLE_OIDC_ENABLED":                    "true",
		"MENDER_BROWSER_SESSION_DATABASE_URL":             "postgres://browser-not-connected",
		"MENDER_CONSOLE_OIDC_ISSUER":                      "https://idp.example",
		"MENDER_CONSOLE_OIDC_CLIENT_ID":                   "mender-console",
		"MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE":          "oidc-client-secret-file",
		"MENDER_CONSOLE_OIDC_REDIRECT_URL":                "https://mender.example/auth/callback",
		"MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE":            "flow-signing-key-file",
		"MENDER_CONSOLE_CONNECTIONS_ENABLED":               "true",
		"MENDER_CONNECTION_MANAGER_DATABASE_URL":           "postgres://connection-manager-not-connected",
		"MENDER_CONSOLE_CONNECTION_OAUTH_ENABLED":          "true",
		"MENDER_CONNECTION_OAUTH_PROVIDER_ID":              "provider_alpha",
		"MENDER_CONNECTION_OAUTH_AUTHORIZATION_URL":        "https://provider.example/oauth/authorize",
		"MENDER_CONNECTION_OAUTH_TOKEN_URL":                "https://provider.example/oauth/token",
		"MENDER_CONNECTION_OAUTH_CLIENT_ID":                "mender-provider-client",
		"MENDER_CONNECTION_OAUTH_CLIENT_SECRET_FILE":       "provider-client-secret-file",
		"MENDER_CONNECTION_OAUTH_REDIRECT_URL":             "https://mender.example/api/console/v1/connections/oauth/callback",
		"MENDER_CONNECTION_OAUTH_SCOPES":                   "resources.read, profile.read",
		"MENDER_CONNECTION_OAUTH_SECRET_ROOT":              "reviewed-secret-root",
		"MENDER_CONNECTION_OAUTH_FLOW_TTL":                 "7m",
	}
	cfg, err := LoadAPIConfig(func(name string) string { return valid[name] })
	if err != nil || !cfg.ConsoleConnectionOAuthEnabled || cfg.ConnectionOAuthProviderID != "provider_alpha" || len(cfg.ConnectionOAuthScopes) != 2 {
		t.Fatal(cfg, err)
	}
	for _, change := range []map[string]string{
		{"MENDER_CONSOLE_CONNECTION_OAUTH_ENABLED": "yes"},
		{"MENDER_CONSOLE_CONNECTIONS_ENABLED": "false"},
		{"MENDER_CONSOLE_OIDC_ENABLED": "false"},
		{"MENDER_CONNECTION_OAUTH_PROVIDER_ID": ""},
		{"MENDER_CONNECTION_OAUTH_TOKEN_URL": ""},
		{"MENDER_CONNECTION_OAUTH_CLIENT_SECRET_FILE": ""},
		{"MENDER_CONNECTION_OAUTH_SCOPES": ""},
		{"MENDER_CONNECTION_OAUTH_SECRET_ROOT": ""},
		{"MENDER_CONNECTION_OAUTH_FLOW_TTL": "30s"},
		{"MENDER_CONNECTION_OAUTH_FLOW_TTL": "16m"},
	} {
		_, err = LoadAPIConfig(func(name string) string {
			if value, ok := change[name]; ok {
				return value
			}
			return valid[name]
		})
		if err == nil {
			t.Fatal("unsafe Connection OAuth configuration accepted", change)
		}
	}
}
