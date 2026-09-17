package localprovider

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubmitThenStatus(t *testing.T) {
	server := New()
	submit := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewBufferString(`{"query":"Acme"}`))
	submit.Header.Set("Content-Type", "application/json")
	submit.Header.Set("Idempotency-Key", "demo-request-0001")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, submit)
	if w.Code != http.StatusAccepted {
		t.Fatal(w.Code, w.Body.String())
	}
	var accepted struct {
		ProviderRequestID string `json:"provider_request_id"`
	}
	if json.Unmarshal(w.Body.Bytes(), &accepted) != nil || accepted.ProviderRequestID == "" {
		t.Fatal("missing request id")
	}

	status := httptest.NewRequest(http.MethodPost, "/status", bytes.NewBufferString(`{"provider_request_id":"`+accepted.ProviderRequestID+`"}`))
	status.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, status)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"state":"succeeded"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"query":"Acme"`)) {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestFixtureRejectsUnknownInput(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewBufferString(`{"query":"Acme","url":"http://example.com"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "demo-request-0002")
	w := httptest.NewRecorder()
	New().Handler().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatal(w.Code)
	}
}
