package oauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	connectionapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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

func TestReviewedOAuthRefreshUsesServerOwnedEndpointAndParsesRotation(t *testing.T) {
	var calls int
	httpClient := &http.Client{Timeout: 5 * time.Second, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.String() != "https://provider.example/oauth/token" || req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatalf("refresh escaped reviewed token endpoint: %s %s", req.Method, req.URL)
		}
		user, password, ok := req.BasicAuth()
		if !ok || user != "mender-client" || password != "mounted-client-secret" {
			t.Fatal("refresh did not use reviewed client authentication")
		}
		body, _ := io.ReadAll(req.Body)
		values, err := url.ParseQuery(string(body))
		if err != nil || values.Get("grant_type") != "refresh_token" || values.Get("refresh_token") != "refresh-alpha" || len(values) != 2 {
			t.Fatal("unexpected OAuth refresh form", string(body), err)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"access-beta","token_type":"Bearer","expires_in":3600,"refresh_token":"refresh-beta","scope":"resources.read profile.read"}`))}, nil
	})}
	client, err := New(reviewedConfig(), httpClient)
	if err != nil {
		t.Fatal(err)
	}
	token, err := client.Refresh(context.Background(), []byte("refresh-alpha"))
	if err != nil || calls != 1 || string(token.Value) != "access-beta" || string(token.RefreshValue) != "refresh-beta" || len(token.Scopes) != 2 || !token.ExpiresAt.After(time.Now().UTC().Add(50*time.Minute)) {
		t.Fatal("reviewed OAuth refresh response was not normalized", token, calls, err)
	}
}

func TestReviewedOAuthRefreshSeparatesPermanentAndTransientFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		permanent bool
	}{
		{"invalid_grant", http.StatusBadRequest, `{"error":"invalid_grant"}`, true},
		{"provider_unavailable", http.StatusServiceUnavailable, `{"error":"temporarily_unavailable"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := New(reviewedConfig(), &http.Client{Timeout: 5 * time.Second, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Refresh(context.Background(), []byte("refresh-alpha"))
			if err == nil || errors.Is(err, connectionapp.ErrOAuthRefreshRejected) != tc.permanent {
				t.Fatal("OAuth refresh failure classification drifted", err)
			}
		})
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
