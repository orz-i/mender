package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/processes/providercallback/application"
)

type testClock struct{ at time.Time }

func (c testClock) Now() time.Time { return c.at }

type testVerifier struct {
	verification application.Verification
	err          error
	calls        int
}

func TestCallbackAcceptsStrictAgentInputRequiredEvent(t *testing.T) {
	at := time.Date(2026, 9, 15, 4, 2, 0, 0, time.UTC)
	verifier := &testVerifier{verification: application.Verification{SignedAt: time.Date(2026, 9, 15, 4, 0, 0, 0, time.UTC), BodySHA256: strings.Repeat("b", 64)}}
	receiver := &testReceiver{receipt: application.Receipt{EventID: "evt.agent.input.1", Disposition: application.Accepted}}
	router := testHandler(t, receiver, verifier, at)
	body := `{"schema_version":1,"event_type":"provider.input_required","event_id":"evt.agent.input.1","workspace_id":"ws_agent","run_id":"run_agent","attempt_no":1,"provider_request_id":"agent/request-1","external_task_id":"agent/task-1","input_request_id":"input.req.1","prompt":"Choose a region","input_schema":{"type":"object","properties":{"region":{"type":"string"}},"required":["region"]},"occurred_at":"2026-09-15T04:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/api/provider-callbacks/v1/providers/provider_agent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Mender-Callback-Key-Id", "key_current")
	req.Header.Set("X-Mender-Callback-Timestamp", "1789444800")
	req.Header.Set("X-Mender-Callback-Signature", "opaque-signature")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted || receiver.calls != 1 || receiver.callback.EventType != "provider.input_required" || receiver.callback.State != "input_required" || receiver.callback.InputRequestID != "input.req.1" || receiver.callback.ObservationID != "input.req.1" || receiver.callback.InputPrompt != "Choose a region" || receiver.callback.InputSchemaJSON == "" {
		t.Fatal(w.Code, w.Body.String(), receiver.callback)
	}
	if receiver.callback.ResultJSON != "" || receiver.callback.ErrorCode != "" || strings.Contains(w.Body.String(), "Choose a region") || strings.Contains(w.Body.String(), "input_schema") {
		t.Fatal("input-required callback leaked provider metadata in response", w.Body.String())
	}
}

func (v *testVerifier) Verify(context.Context, string, string, string, string, []byte, time.Time) (application.Verification, error) {
	v.calls++
	return v.verification, v.err
}

type testReceiver struct {
	callback application.Callback
	receipt  application.Receipt
	err      error
	calls    int
}

func (r *testReceiver) IngestProviderCallback(_ context.Context, callback application.Callback) (application.Receipt, error) {
	r.calls++
	r.callback = callback
	return r.receipt, r.err
}

func payload() string {
	return `{"schema_version":1,"event_type":"provider.observation","event_id":"evt.agent.1","workspace_id":"ws_agent","run_id":"run_agent","attempt_no":1,"provider_request_id":"agent/request-1","external_task_id":"agent/task-1","observation_id":"obs.agent.1","state":"succeeded","result":{"answer":42},"error_code":null,"occurred_at":"2026-09-14T03:00:00Z"}`
}

func testHandler(t *testing.T, receiver *testReceiver, verifier application.Verifier, at time.Time) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service, err := application.New(receiver, verifier, testClock{at: at})
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(service)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	h.Register(router)
	return router
}

func TestCallbackVerifiesRawBodyBeforeStrictParsingAndForwardsSafeFacts(t *testing.T) {
	at := time.Date(2026, 9, 14, 3, 2, 0, 0, time.UTC)
	verifier := &testVerifier{verification: application.Verification{SignedAt: time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC), BodySHA256: strings.Repeat("a", 64)}}
	receiver := &testReceiver{receipt: application.Receipt{EventID: "evt.agent.1", Disposition: application.Accepted}}
	router := testHandler(t, receiver, verifier, at)
	body := payload()
	timestamp := "1789354800"
	req := httptest.NewRequest(http.MethodPost, "/api/provider-callbacks/v1/providers/provider_agent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Mender-Callback-Key-Id", "key_current")
	req.Header.Set("X-Mender-Callback-Timestamp", timestamp)
	req.Header.Set("X-Mender-Callback-Signature", "opaque-signature")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted || receiver.calls != 1 || verifier.calls != 1 {
		t.Fatal(w.Code, w.Body.String(), receiver.calls, verifier.calls)
	}
	if receiver.callback.ProviderID != "provider_agent" || receiver.callback.EventID != "evt.agent.1" || receiver.callback.ResultJSON != `{"answer":42}` || len(receiver.callback.BodySHA256) != 64 || receiver.callback.KeyID != "key_current" {
		t.Fatal(receiver.callback)
	}
	if strings.Contains(w.Body.String(), "agent/task-1") || strings.Contains(w.Body.String(), "answer") || strings.Contains(w.Body.String(), "key_current") {
		t.Fatal("callback response leaked internal callback facts", w.Body.String())
	}
}

func TestCallbackRejectsTamperStaleSignatureAndDuplicateJSONBeforeReceiver(t *testing.T) {
	at := time.Date(2026, 9, 14, 3, 2, 0, 0, time.UTC)
	tests := map[string]struct {
		body     string
		verifier *testVerifier
	}{
		"tamper":        {payload(), &testVerifier{err: application.ErrUnauthorized}},
		"stale":         {payload(), &testVerifier{err: application.ErrUnauthorized}},
		"duplicate-key": {strings.Replace(payload(), `"event_id":"evt.agent.1"`, `"event_id":"evt.agent.1","event_id":"evt.agent.2"`, 1), &testVerifier{verification: application.Verification{SignedAt: at, BodySHA256: strings.Repeat("a", 64)}}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			receiver := &testReceiver{}
			router := testHandler(t, receiver, tc.verifier, at)
			req := httptest.NewRequest(http.MethodPost, "/api/provider-callbacks/v1/providers/provider_agent", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Mender-Callback-Key-Id", "key_current")
			req.Header.Set("X-Mender-Callback-Timestamp", "1789354800")
			req.Header.Set("X-Mender-Callback-Signature", "opaque-signature")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code == http.StatusAccepted || w.Code == http.StatusOK || receiver.calls != 0 || tc.verifier.calls != 1 {
				t.Fatal(w.Code, receiver.calls, tc.verifier.calls, w.Body.String())
			}
		})
	}
}

func TestCallbackDuplicateReturns200AndReceiverFailuresStayOpaque(t *testing.T) {
	at := time.Date(2026, 9, 14, 3, 2, 0, 0, time.UTC)
	verifier := &testVerifier{verification: application.Verification{SignedAt: time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC), BodySHA256: strings.Repeat("a", 64)}}
	timestamp := "1789354800"
	body := payload()
	receiver := &testReceiver{receipt: application.Receipt{EventID: "evt.agent.1", Disposition: application.Duplicate}}
	router := testHandler(t, receiver, verifier, at)
	req := httptest.NewRequest(http.MethodPost, "/api/provider-callbacks/v1/providers/provider_agent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Mender-Callback-Key-Id", "key_current")
	req.Header.Set("X-Mender-Callback-Timestamp", timestamp)
	req.Header.Set("X-Mender-Callback-Signature", "opaque-signature")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}

	receiver = &testReceiver{err: errors.New("postgres secret detail")}
	verifier = &testVerifier{verification: application.Verification{SignedAt: time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC), BodySHA256: strings.Repeat("a", 64)}}
	router = testHandler(t, receiver, verifier, at)
	req = httptest.NewRequest(http.MethodPost, "/api/provider-callbacks/v1/providers/provider_agent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Mender-Callback-Key-Id", "key_current")
	req.Header.Set("X-Mender-Callback-Timestamp", timestamp)
	req.Header.Set("X-Mender-Callback-Signature", "opaque-signature")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "postgres") {
		t.Fatal(w.Code, w.Body.String())
	}
}
