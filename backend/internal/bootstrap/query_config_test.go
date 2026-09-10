package bootstrap

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http/httptest"
	"testing"
)

func TestRunReadFeatureRequiresExplicitScopeAndProtectedSigningKey(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32))
	valid := map[string]string{"MENDER_RUN_API_ENABLED": "true", "MENDER_RUN_READ_API_ENABLED": "true", "MENDER_DATABASE_URL": "postgres://not-connected", "MENDER_CURSOR_SIGNING_KEY": key}
	cfg, e := LoadAPIConfig(func(k string) string { return valid[k] })
	if e != nil || !cfg.RunReadAPIEnabled || len(cfg.CursorSigningKey) != 32 {
		t.Fatal(e)
	}
	for _, change := range []map[string]string{{"MENDER_RUN_API_ENABLED": "false"}, {"MENDER_RUN_READ_API_ENABLED": "yes"}, {"MENDER_CURSOR_SIGNING_KEY": ""}, {"MENDER_CURSOR_SIGNING_KEY": "malformed"}, {"MENDER_CURSOR_SIGNING_KEY": base64.RawURLEncoding.EncodeToString(make([]byte, 32))}} {
		_, e = LoadAPIConfig(func(k string) string {
			if v, ok := change[k]; ok {
				return v
			}
			return valid[k]
		})
		if e == nil {
			t.Fatal("unsafe query configuration accepted")
		}
	}
	for _, cfg := range []APIConfig{{RunReadAPIEnabled: true}, {RunAPIEnabled: true, RunReadAPIEnabled: true}} {
		if h, _, e := BuildAPI(context.Background(), cfg); e == nil || h != nil {
			t.Fatal("misconfiguration silently downgraded")
		}
	}
	h, closeIt, e := BuildAPI(context.Background(), APIConfig{})
	if e != nil {
		t.Fatal(e)
	}
	defer closeIt()
	for _, path := range []string{"/api/v1/workspaces/ws_a/runs", "/api/v1/workspaces/ws_a/runs/run_a/events"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatal("query route enabled by default", w.Code)
		}
	}
}
