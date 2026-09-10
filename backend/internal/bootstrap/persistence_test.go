package bootstrap

import (
	"bytes"
	"context"
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
