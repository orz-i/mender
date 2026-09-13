package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/processes/catalogmanagement/application"
)

type testAuth struct{ forbid bool }

func (a *testAuth) Authenticate(context.Context, string) (application.Actor, error) {
	return application.Actor{UserID: "user_1"}, nil
}
func (a *testAuth) AuthenticateMutation(_ context.Context, _ string, csrf string) (application.Actor, error) {
	if csrf != "csrf_1" {
		return application.Actor{}, application.ErrForbidden
	}
	return application.Actor{UserID: "user_1"}, nil
}
func (a *testAuth) Authorize(context.Context, application.Actor, string, string) error {
	if a.forbid {
		return application.ErrForbidden
	}
	return nil
}

type testClock struct{ at time.Time }

func (c testClock) Now() time.Time { return c.at }

type testRepo struct {
	preflight   application.Preflight
	createCalls int
}

func (r *testRepo) Snapshot(context.Context, string, time.Time) (application.Snapshot, error) {
	return application.Snapshot{ToolVersions: []application.ToolVersion{}, Toolsets: []application.Toolset{}, Connections: []application.ConnectionOption{}, Prices: []application.PriceOption{{ID: "price_1", ToolVersionID: "tv_1", Currency: "USD", ReserveMicro: 9007199254740993, Active: true}}, Budgets: []application.BudgetOption{}}, nil
}
func (r *testRepo) CreateToolVersion(_ context.Context, ws string, in application.ToolVersionInput) (application.ToolVersion, error) {
	r.createCalls++
	return application.ToolVersion{WorkspaceID: ws, ToolVersionID: in.ToolVersionID, ToolID: in.ToolID, Version: in.Version, ProviderID: in.ProviderID, PriceVersionID: in.PriceVersionID, DeploymentRevision: in.DeploymentRevision, Title: in.Title, Description: in.Description, InputSchema: in.InputSchema, OutputSchema: in.OutputSchema, SideEffect: in.SideEffect, Idempotency: in.Idempotency, MCPPublishable: in.MCPPublishable, State: "draft", CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
}
func (*testRepo) UpdateToolVersion(context.Context, string, string, application.ToolVersionInput) (application.ToolVersion, error) {
	return application.ToolVersion{}, application.ErrUnavailable
}
func (r *testRepo) ToolVersionPreflight(context.Context, string, string, time.Time) (application.Preflight, error) {
	return r.preflight, nil
}
func (*testRepo) PublishToolVersion(context.Context, string, string, time.Time) (application.ToolVersion, error) {
	return application.ToolVersion{}, application.ErrUnavailable
}
func (*testRepo) RetireToolVersion(context.Context, string, string, time.Time) (application.ToolVersion, error) {
	return application.ToolVersion{}, application.ErrUnavailable
}
func (*testRepo) CreateToolset(context.Context, string, string) (application.Toolset, error) {
	return application.Toolset{}, application.ErrUnavailable
}
func (*testRepo) UpsertBinding(context.Context, string, string, application.BindingInput) (application.Binding, error) {
	return application.Binding{}, application.ErrUnavailable
}
func (*testRepo) DeleteBinding(context.Context, string, string, string) error {
	return application.ErrUnavailable
}
func (r *testRepo) ToolsetPreflight(context.Context, string, string, time.Time) (application.Preflight, error) {
	return r.preflight, nil
}
func (*testRepo) PublishToolset(context.Context, string, string, time.Time) (application.Toolset, error) {
	return application.Toolset{}, application.ErrUnavailable
}
func (*testRepo) RetireToolset(context.Context, string, string, time.Time) (application.Toolset, error) {
	return application.Toolset{}, application.ErrUnavailable
}

func testRouter(t *testing.T, repo *testRepo, auth *testAuth) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service, err := application.New(repo, auth, testClock{at: time.Date(2026, 9, 12, 20, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(service, auth)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	handler.Register(r)
	return r
}
func request(r http.Handler, method, path, body, csrf string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session_1"})
	if csrf != "" {
		req.Header.Set("X-Mender-CSRF", csrf)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSnapshotSerializesMicroAmountsAsStrings(t *testing.T) {
	repo := &testRepo{}
	w := request(testRouter(t, repo, &testAuth{}), http.MethodGet, "/api/console/v1/workspaces/ws_1/catalog", "", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"reserve_micro":"9007199254740993"`) {
		t.Fatal("reserve_micro was not serialized as fixed-precision string", w.Body.String())
	}
}

func TestMutationRequiresCSRFAndStrictJSON(t *testing.T) {
	repo := &testRepo{}
	r := testRouter(t, repo, &testAuth{})
	body := `{"tool_version_id":"tv_1","tool_id":"tool_1","version":"1.0.0","provider_id":"provider_1","price_version_id":"price_1","deployment_revision":"deploy_1","title":"Search","description":"","input_schema":{"type":"object"},"output_schema":{"type":"object"},"side_effect":"read_only","idempotency":"safe_read","mcp_publishable":false}`
	if w := request(r, http.MethodPost, "/api/console/v1/workspaces/ws_1/catalog/tool-versions", body, ""); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if repo.createCalls != 0 {
		t.Fatal("mutation reached repository without CSRF")
	}
	bad := strings.TrimSuffix(body, "}") + `,"unexpected":true}`
	if w := request(r, http.MethodPost, "/api/console/v1/workspaces/ws_1/catalog/tool-versions", bad, "csrf_1"); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if repo.createCalls != 0 {
		t.Fatal("unknown field reached repository")
	}
	if w := request(r, http.MethodPost, "/api/console/v1/workspaces/ws_1/catalog/tool-versions", body, "csrf_1"); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if repo.createCalls != 1 {
		t.Fatal("valid mutation not persisted")
	}
}

func TestPreflightResponseIsServerOwned(t *testing.T) {
	repo := &testRepo{preflight: application.Preflight{Ready: false, Issues: []application.Issue{{Code: "budget_unavailable", TargetID: "budget_1"}}}}
	w := request(testRouter(t, repo, &testAuth{}), http.MethodPost, "/api/console/v1/workspaces/ws_1/catalog/toolsets/set_1/preflight", "", "csrf_1")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"ready":false`) || !strings.Contains(w.Body.String(), `"code":"budget_unavailable"`) {
		t.Fatal("authoritative preflight facts missing", w.Body.String())
	}
}

func TestReadAuthorizationFailureIsForbidden(t *testing.T) {
	w := request(testRouter(t, &testRepo{}, &testAuth{forbid: true}), http.MethodGet, "/api/console/v1/workspaces/ws_1/catalog", "", "")
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}
