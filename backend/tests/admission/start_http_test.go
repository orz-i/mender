package admission_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	admissionhttp "github.com/orz-i/mender/backend/internal/processes/admission/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
)

type authnFunc func(context.Context, string) (application.Caller, error)

func (f authnFunc) Authenticate(ctx context.Context, token string) (application.Caller, error) {
	return f(ctx, token)
}

func startBody(extra string) string {
	body := `{"tool_ref":{"tool_id":"tool_a","version":"1.0.0"},"toolset_id":"set_v1","connection_id":"conn_a","arguments":{"n":1},"max_charge":{"currency":"USD","amount_micro":"100"},"wait_ms":0}`
	if extra == "" {
		return body
	}
	return strings.TrimSuffix(body, "}") + "," + extra + "}"
}

func TestStartRunHTTPIsAuthenticatedStrictAndReplaySafe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	u := &unit{scope: &scope{}}
	svc := build(t, u, allow, resolve)
	authn := authnFunc(func(ctx context.Context, token string) (application.Caller, error) {
		if err := ctx.Err(); err != nil {
			return application.Caller{}, err
		}
		if token != "token" {
			return application.Caller{}, application.ErrUnauthenticated
		}
		return who, nil
	})
	h, err := admissionhttp.New(svc, authn)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	h.Register(router)
	requestHTTP := func(path, body string, configure func(*http.Request)) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer token")
		r.Header.Set("Idempotency-Key", "request_0001")
		if configure != nil {
			configure(r)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	w := requestHTTP("/api/v1/workspaces/ws_a/runs", startBody(""), nil)
	if w.Code != 202 || !strings.Contains(w.Body.String(), `"execution_state":"queued"`) || !strings.Contains(w.Body.String(), `"billing_state":"reserved"`) || !strings.Contains(w.Body.String(), `"run_id":"run_test"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	u.scope = &scope{old: u.scope.captured, found: true}
	w = requestHTTP("/api/v1/workspaces/ws_a/runs", startBody(""), nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"run_id":"run_test"`) {
		t.Fatal(w.Code, w.Body.String())
	}

	cases := []struct {
		name, path, body string
		configure        func(*http.Request)
		want             int
	}{
		{"missing auth", "/api/v1/workspaces/ws_a/runs", startBody(""), func(r *http.Request) { r.Header.Del("Authorization") }, 401},
		{"wrong workspace", "/api/v1/workspaces/ws_b/runs", startBody(""), nil, 403},
		{"missing idempotency", "/api/v1/workspaces/ws_a/runs", startBody(""), func(r *http.Request) { r.Header.Del("Idempotency-Key") }, 400},
		{"multiple idempotency", "/api/v1/workspaces/ws_a/runs", startBody(""), func(r *http.Request) { r.Header.Add("Idempotency-Key", "request_0002") }, 400},
		{"wrong media", "/api/v1/workspaces/ws_a/runs", startBody(""), func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
		{"client budget override", "/api/v1/workspaces/ws_a/runs", startBody(`"budget_id":"attacker_budget"`), nil, 400},
		{"duplicate arguments", "/api/v1/workspaces/ws_a/runs", strings.Replace(startBody(""), `"arguments":{"n":1}`, `"arguments":{"n":1,"n":2}`, 1), nil, 400},
		{"duplicate nested contract", "/api/v1/workspaces/ws_a/runs", strings.Replace(startBody(""), `"tool_id":"tool_a"`, `"tool_id":"tool_a","tool_id":"tool_b"`, 1), nil, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := requestHTTP(tc.path, tc.body, tc.configure)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d: %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	large := strings.Replace(startBody(""), `"arguments":{"n":1}`, `"arguments":{"text":"`+strings.Repeat("x", 80000)+`"}`, 1)
	w = requestHTTP("/api/v1/workspaces/ws_a/runs", large, nil)
	if w.Code != 413 {
		t.Fatal("oversized body accepted", w.Code, w.Body.String())
	}
}
