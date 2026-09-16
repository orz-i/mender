package bootstrap

import "testing"

func paymentOIDCBase() map[string]string {
	return map[string]string{
		"MENDER_RUN_API_ENABLED":                 "true",
		"MENDER_DATABASE_URL":                    "postgres://runtime-not-connected",
		"MENDER_CONSOLE_OIDC_ENABLED":            "true",
		"MENDER_BROWSER_SESSION_DATABASE_URL":    "postgres://browser-session-not-connected",
		"MENDER_CONSOLE_OIDC_ISSUER":             "https://idp.example",
		"MENDER_CONSOLE_OIDC_CLIENT_ID":          "mender-console",
		"MENDER_CONSOLE_OIDC_CLIENT_SECRET_FILE": "oidc-client-secret-file",
		"MENDER_CONSOLE_OIDC_REDIRECT_URL":       "https://mender.example/auth/callback",
		"MENDER_CONSOLE_FLOW_SIGNING_KEY_FILE":   "flow-signing-key-file",
		"MENDER_PAYMENT_MODE":                    "sandbox",
		"MENDER_ADMIN_PAYMENTS_ENABLED":          "true",
		"MENDER_PAYMENT_MANAGER_DATABASE_URL":    "postgres://payment-manager-not-connected",
	}
}

func TestAdminPaymentsAreSandboxOnlyAndRequireOIDCAndSeparateRole(t *testing.T) {
	base := paymentOIDCBase()
	cfg, err := LoadAPIConfig(func(key string) string { return base[key] })
	if err != nil || !cfg.AdminPaymentsEnabled || cfg.PaymentMode != "sandbox" || cfg.PaymentManagerDatabaseURL == "" {
		t.Fatal(cfg, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"MENDER_PAYMENT_MODE", "live"},
		{"MENDER_ADMIN_PAYMENTS_ENABLED", "yes"},
		{"MENDER_CONSOLE_OIDC_ENABLED", "false"},
		{"MENDER_PAYMENT_MANAGER_DATABASE_URL", ""},
	} {
		copy := make(map[string]string, len(base))
		for k, v := range base {
			copy[k] = v
		}
		copy[tc.key] = tc.value
		if _, err = LoadAPIConfig(func(key string) string { return copy[key] }); err == nil {
			t.Fatal("invalid Admin Payments config accepted", tc.key, tc.value)
		}
	}
}

func TestSandboxPaymentCallbacksRequireReviewedAccountKeys(t *testing.T) {
	base := map[string]string{
		"MENDER_RUN_API_ENABLED":                        "true",
		"MENDER_DATABASE_URL":                           "postgres://runtime-not-connected",
		"MENDER_PAYMENT_MODE":                           "sandbox",
		"MENDER_SANDBOX_PAYMENT_CALLBACKS_ENABLED":      "true",
		"MENDER_PAYMENT_CALLBACK_INGESTOR_DATABASE_URL": "postgres://payment-callback-not-connected",
		"MENDER_PAYMENT_CALLBACK_SECRET_ROOT":           "payment-callback-secret-root",
		"MENDER_PAYMENT_CALLBACK_REVIEWED_KEYS":         "sandbox_psp/account.one=key_current|key_previous",
	}
	cfg, err := LoadAPIConfig(func(key string) string { return base[key] })
	if err != nil || !cfg.SandboxPaymentCallbacksEnabled || cfg.PaymentMode != "sandbox" || len(cfg.PaymentCallbackReviewedKeys["sandbox_psp/account.one"]) != 2 {
		t.Fatal(cfg, err)
	}
	for _, tc := range []struct{ key, value string }{
		{"MENDER_PAYMENT_MODE", "live"},
		{"MENDER_SANDBOX_PAYMENT_CALLBACKS_ENABLED", "yes"},
		{"MENDER_PAYMENT_CALLBACK_INGESTOR_DATABASE_URL", ""},
		{"MENDER_PAYMENT_CALLBACK_SECRET_ROOT", ""},
		{"MENDER_PAYMENT_CALLBACK_REVIEWED_KEYS", "sandbox_psp=key_current"},
		{"MENDER_PAYMENT_CALLBACK_REVIEWED_KEYS", "sandbox_psp/account.one=bad.key"},
	} {
		copy := make(map[string]string, len(base))
		for k, v := range base {
			copy[k] = v
		}
		copy[tc.key] = tc.value
		if _, err = LoadAPIConfig(func(key string) string { return copy[key] }); err == nil {
			t.Fatal("invalid payment callback config accepted", tc.key, tc.value)
		}
	}
}
