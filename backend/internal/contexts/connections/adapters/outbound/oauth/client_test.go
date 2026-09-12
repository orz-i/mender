package oauth

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func reviewedConfig() Config {
	return Config{
		ProviderID:       "provider_alpha",
		AuthorizationURL: "https://provider.example/oauth/authorize",
		TokenURL:         "https://provider.example/oauth/token",
		ClientID:         "mender-client",
		ClientSecret:     "mounted-client-secret",
		RedirectURL:      "https://mender.example/api/console/v1/connections/oauth/callback",
		Scopes:           []string{"resources.read", "profile.read"},
	}
}

func TestReviewedOAuthProviderUsesPKCES256AndExactServerConfig(t *testing.T) {
	client, err := New(reviewedConfig(), &http.Client{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := client.AuthorizationURL("state-alpha", strings.Repeat("v", 43))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "provider.example" || u.Query().Get("state") != "state-alpha" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("code_challenge") == "" {
		t.Fatal("reviewed OAuth authorization URL lost PKCE or exact endpoint", raw)
	}
}

func TestReviewedOAuthProviderRejectsUnsafeEndpointsAndScopes(t *testing.T) {
	for _, mutate := range []func(*Config){
		func(c *Config) { c.AuthorizationURL = "http://provider.example/oauth/authorize" },
		func(c *Config) { c.TokenURL = "https://127.0.0.1/oauth/token" },
		func(c *Config) { c.TokenURL = "https://localhost/oauth/token" },
		func(c *Config) { c.RedirectURL = "https://mender.example/wrong/callback" },
		func(c *Config) { c.Scopes = []string{"resources.read", "resources.read"} },
		func(c *Config) { c.Scopes = []string{"resources read"} },
	} {
		config := reviewedConfig()
		mutate(&config)
		if client, err := New(config, &http.Client{Timeout: 5 * time.Second}); err == nil || client != nil {
			t.Fatal("unsafe OAuth provider configuration accepted", config)
		}
	}
}
