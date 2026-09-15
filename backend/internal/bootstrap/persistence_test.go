package bootstrap

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http/httptest"
	"testing"
)

func TestRunAPIDefaultIsClosedAndBadConfigFails(t *testing.T) {
	get := func(string) string { return "" }
	cfg, err := LoadAPIConfig(get)
	if err != nil || cfg.RunAPIEnabled {
		t.Fatal(err)
	}
	h, closeIt, err := BuildAPI(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer closeIt()
	for path, want := range map[string]int{"/healthz": 200, "/readyz": 503, "/api/v1/workspaces/ws_a/runs/run_1": 404} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	for _, flag := range []string{"true", "TRUE", "yes", "1"} {
		_, err := LoadAPIConfig(func(k string) string {
			if k == "MENDER_RUN_API_ENABLED" {
				return flag
			}
			return ""
		})
		if err == nil {
			t.Fatal("invalid/missing config accepted", flag)
		}
	}
	if h, _, err = BuildAPI(context.Background(), APIConfig{RunAPIEnabled: true}); err == nil || h != nil {
		t.Fatal("enabled API silently fell back")
	}
}

func TestAgentInputAPIConfigIsExplicitAndFailsClosed(t *testing.T) {
	base := map[string]string{
		"MENDER_RUN_API_ENABLED":                   "true",
		"MENDER_RUN_READ_API_ENABLED":              "true",
		"MENDER_CURSOR_SIGNING_KEY":                base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)),
		"MENDER_DATABASE_URL":                      "postgres://runtime:pw@127.0.0.1:5432/mender?sslmode=disable",
		"MENDER_AGENT_INPUT_API_ENABLED":           "true",
		"MENDER_AGENT_INPUT_DATABASE_URL":          "postgres://agent_input:pw@127.0.0.1:5432/mender?sslmode=disable",
		"MENDER_AGENT_INPUT_EXECUTOR_DATABASE_URL": "postgres://executor:pw@127.0.0.1:5432/mender?sslmode=disable",
		"MENDER_AGENT_INPUT_SECRET_ROOT":           `C:\mounted-agent-secrets`,
		"MENDER_AGENT_INPUT_PROVIDER_IDS":          "provider_agent",
		"MENDER_AGENT_INPUT_ALLOWED_HOSTS":         "agent.example",
	}
	get := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	cfg, err := LoadAPIConfig(get(base))
	if err != nil || !cfg.AgentInputAPIEnabled || len(cfg.AgentInputProviderIDs) != 1 || len(cfg.AgentInputAllowedHosts) != 1 || cfg.AgentInputAllowHTTP || cfg.AgentInputAllowLoopback {
		t.Fatal(cfg, err)
	}
	for _, key := range []string{"MENDER_AGENT_INPUT_DATABASE_URL", "MENDER_AGENT_INPUT_EXECUTOR_DATABASE_URL", "MENDER_AGENT_INPUT_SECRET_ROOT", "MENDER_AGENT_INPUT_PROVIDER_IDS", "MENDER_AGENT_INPUT_ALLOWED_HOSTS"} {
		values := map[string]string{}
		for k, v := range base {
			values[k] = v
		}
		delete(values, key)
		if _, err = LoadAPIConfig(get(values)); err == nil {
			t.Fatal("Agent input API accepted missing required configuration", key)
		}
	}
	values := map[string]string{}
	for k, v := range base {
		values[k] = v
	}
	values["MENDER_AGENT_INPUT_ALLOW_HTTP"] = "yes"
	if _, err = LoadAPIConfig(get(values)); err == nil {
		t.Fatal("Agent input API accepted non-strict egress boolean")
	}
}

func TestOperatorDoesNotUseAPICredentialsOrLeakUnstoredKey(t *testing.T) {
	var out, errOut bytes.Buffer
	get := func(k string) string {
		if k == "MENDER_DATABASE_URL" {
			return "not-an-admin-secret"
		}
		return ""
	}
	for _, args := range [][]string{{"migrate"}, {"issue-key", "--workspace", "ws_a", "--subject", "sa_a"}, {"issue-key", "--workspace", "../ws", "--subject", "sa_a"}, {"issue-key", "--workspace", "ws_a", "--subject", "sa_a", "--ttl", "0s"}, {"unknown"}} {
		if err := RunOperator(context.Background(), args, get, &out, &errOut); err == nil {
			t.Fatal("operator proceeded without authorized configuration")
		}
		if out.Len() != 0 {
			t.Fatal("secret/success printed before persistence")
		}
	}
}
