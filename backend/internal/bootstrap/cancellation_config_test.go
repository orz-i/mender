package bootstrap

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestCoordinatedCancelConfigurationIsExplicitAndFailsClosed(t *testing.T) {
	valid := map[string]string{"MENDER_RUN_API_ENABLED": "true", "MENDER_DATABASE_URL": "postgres://not-connected-reader", "MENDER_RUN_COORDINATED_CANCEL_ENABLED": "true", "MENDER_CANCELLATION_DATABASE_URL": "postgres://not-connected-canceler"}
	c, e := LoadAPIConfig(func(k string) string { return valid[k] })
	if e != nil || !c.CoordinatedCancelEnabled || c.CancellationDatabaseURL != valid["MENDER_CANCELLATION_DATABASE_URL"] {
		t.Fatal(c, e)
	}
	for _, override := range []map[string]string{{"MENDER_RUN_API_ENABLED": "false"}, {"MENDER_RUN_API_ENABLED": ""}, {"MENDER_RUN_COORDINATED_CANCEL_ENABLED": "yes"}, {"MENDER_CANCELLATION_DATABASE_URL": ""}, {"MENDER_DATABASE_URL": ""}} {
		_, e = LoadAPIConfig(func(k string) string {
			if v, ok := override[k]; ok {
				return v
			}
			return valid[k]
		})
		if e == nil {
			t.Fatal("invalid feature combination accepted", override)
		}
	}
	c, e = LoadAPIConfig(func(k string) string {
		if k == "MENDER_CANCELLATION_DATABASE_URL" {
			return "ignored-invalid-url"
		}
		return ""
	})
	if e != nil || c.CoordinatedCancelEnabled || c.CancellationDatabaseURL != "" {
		t.Fatal("disabled feature consumed writer configuration", c, e)
	}
	if h, _, e := BuildAPI(context.Background(), APIConfig{CoordinatedCancelEnabled: true}); e == nil || h != nil {
		t.Fatal("orphan feature flag accepted")
	}
	h, closeIt, e := BuildAPI(context.Background(), APIConfig{})
	if e != nil {
		t.Fatal(e)
	}
	defer closeIt()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/workspaces/ws_a/runs/run_a/cancel", nil))
	if w.Code != 404 {
		t.Fatal("default mutation route enabled", w.Code)
	}
}
