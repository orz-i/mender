package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeSemantics(t *testing.T) {
	router := NewRouter()
	for _, tc := range []struct {
		path   string
		code   int
		status string
	}{
		{"/healthz", http.StatusOK, "ok"},
		{"/readyz", http.StatusServiceUnavailable, "not_ready"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != tc.code {
				t.Fatalf("status = %d, want %d", response.Code, tc.code)
			}
			var body healthResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Service != "mender-api" || body.Status != tc.status || body.Stage != "bootstrap" {
				t.Fatalf("unexpected probe: %+v", body)
			}
		})
	}
}

func TestBusinessEndpointsAreNotExposed(t *testing.T) {
	response := httptest.NewRecorder()
	NewRouter().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/runs", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("unimplemented endpoint returned %d", response.Code)
	}
}
