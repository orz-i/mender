package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/processes/catalogmanagement/application"
)

type fakeOpenAPIAnalyzer struct {
	preview application.OpenAPIPreview
	err     error
	raw     string
}

func (a *fakeOpenAPIAnalyzer) Analyze(_ context.Context, raw []byte) (application.OpenAPIPreview, error) {
	a.raw = string(raw)
	return a.preview, a.err
}

func validOpenAPIPreviewFixture() application.OpenAPIPreview {
	return application.OpenAPIPreview{
		OpenAPIVersion: "3.1.0", Title: "Search API",
		Operations: []application.OpenAPIOperation{{
			OperationID: "searchCompanies", Method: "POST", Path: "/search", ServerURL: "https://api.example.test/v1",
			Title: "Search companies", Description: "Search a reviewed directory.", InputSchema: `{"type":"object","additionalProperties":false,"properties":{"query":{"type":"string"}},"required":["query"]}`,
			OutputSchema: `{"type":"object","properties":{"items":{"type":"array","items":{"type":"string"}}}}`, SideEffect: "write", Idempotency: "unsafe", Importable: true,
			Diagnostics: []application.OpenAPIDiagnostic{{Code: "security_runtime_owned", Severity: "warning", OperationID: "searchCompanies", Path: "/search", Method: "POST", Message: "Runtime credentials remain server-owned."}},
		}},
	}
}

func openAPIImportRouter(t *testing.T, repo *testRepo, auth *testAuth, analyzer application.OpenAPIAnalyzer) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	catalog, err := application.New(repo, auth, testClock{at: time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)}, testIDs{})
	if err != nil {
		t.Fatal(err)
	}
	service, err := application.NewOpenAPIImport(catalog, analyzer)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewOpenAPIImport(service, auth)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router)
	return router
}

func TestOpenAPIPreviewReturnsServerFactsWithoutEchoingDocument(t *testing.T) {
	repo := &testRepo{}
	analyzer := &fakeOpenAPIAnalyzer{preview: validOpenAPIPreviewFixture()}
	router := openAPIImportRouter(t, repo, &testAuth{}, analyzer)
	document := "raw-document-marker"
	w := request(router, http.MethodPost, "/api/console/v1/workspaces/ws_1/catalog/openapi/preview", `{"document":"`+document+`"}`, "")
	if w.Code != http.StatusOK || analyzer.raw != document || repo.createCalls != 0 {
		t.Fatal(w.Code, analyzer.raw, repo.createCalls, w.Body.String())
	}
	body := w.Body.String()
	for _, required := range []string{`"openapi_version":"3.1.0"`, `"operation_id":"searchCompanies"`, `"importable":true`, `"side_effect":"write"`, `"idempotency":"unsafe"`} {
		if !strings.Contains(body, required) {
			t.Fatal("missing OpenAPI preview server fact", required, body)
		}
	}
	if strings.Contains(body, document) {
		t.Fatal("OpenAPI preview echoed the raw source document", body)
	}
}

func TestOpenAPIImportRequiresCSRFAndCreatesDraftOnly(t *testing.T) {
	repo := &testRepo{}
	analyzer := &fakeOpenAPIAnalyzer{preview: validOpenAPIPreviewFixture()}
	router := openAPIImportRouter(t, repo, &testAuth{}, analyzer)
	body := `{"document":"doc","operation_id":"searchCompanies","tool_version_id":"tv_import","tool_id":"tool_import","version":"1.0.0","provider_id":"provider_1","price_version_id":"price_1","deployment_revision":"deploy_1"}`
	if w := request(router, http.MethodPost, "/api/console/v1/workspaces/ws_1/catalog/openapi/import", body, ""); w.Code != http.StatusForbidden || repo.createCalls != 0 {
		t.Fatal(w.Code, repo.createCalls, w.Body.String())
	}
	w := request(router, http.MethodPost, "/api/console/v1/workspaces/ws_1/catalog/openapi/import", body, "csrf_1")
	if w.Code != http.StatusCreated || repo.createCalls != 1 {
		t.Fatal(w.Code, repo.createCalls, w.Body.String())
	}
	response := w.Body.String()
	for _, required := range []string{`"tool_version_id":"tv_import"`, `"state":"draft"`, `"mcp_publishable":false`, `"side_effect":"write"`, `"idempotency":"unsafe"`, `"operation_id":"searchCompanies"`} {
		if !strings.Contains(response, required) {
			t.Fatal("missing imported draft fact", required, response)
		}
	}
	if strings.Contains(response, `"document"`) {
		t.Fatal("OpenAPI import echoed source document", response)
	}
}

func TestOpenAPIImportRejectsUnsupportedSelectionAndClientControlFields(t *testing.T) {
	preview := validOpenAPIPreviewFixture()
	preview.Operations[0].Importable = false
	preview.Operations[0].Diagnostics = []application.OpenAPIDiagnostic{{Code: "http_method_unsupported", Severity: "error", OperationID: "searchCompanies", Path: "/search", Method: "GET", Message: "Unsupported."}}
	repo := &testRepo{}
	router := openAPIImportRouter(t, repo, &testAuth{}, &fakeOpenAPIAnalyzer{preview: preview})
	base := `{"document":"doc","operation_id":"searchCompanies","tool_version_id":"tv_import","tool_id":"tool_import","version":"1.0.0","provider_id":"provider_1","price_version_id":"price_1","deployment_revision":"deploy_1"}`
	if w := request(router, http.MethodPost, "/api/console/v1/workspaces/ws_1/catalog/openapi/import", base, "csrf_1"); w.Code != http.StatusBadRequest || repo.createCalls != 0 {
		t.Fatal(w.Code, repo.createCalls, w.Body.String())
	}
	for _, injected := range []string{`"side_effect":"read_only"`, `"mcp_publishable":true`, `"credential_version_ref":"secret"`} {
		body := strings.TrimSuffix(base, "}") + "," + injected + "}"
		if w := request(router, http.MethodPost, "/api/console/v1/workspaces/ws_1/catalog/openapi/import", body, "csrf_1"); w.Code != http.StatusBadRequest || repo.createCalls != 0 {
			t.Fatal("client control field accepted", injected, w.Code, repo.createCalls, w.Body.String())
		}
	}
}

func TestOpenAPIPreviewFailsClosedOnMalformedAnalyzerProjection(t *testing.T) {
	preview := validOpenAPIPreviewFixture()
	preview.Operations[0].InputSchema = `{"type":"array"}`
	repo := &testRepo{}
	router := openAPIImportRouter(t, repo, &testAuth{}, &fakeOpenAPIAnalyzer{preview: preview})
	w := request(router, http.MethodPost, "/api/console/v1/workspaces/ws_1/catalog/openapi/preview", `{"document":"doc"}`, "")
	if w.Code != http.StatusServiceUnavailable || repo.createCalls != 0 {
		t.Fatal(w.Code, repo.createCalls, w.Body.String())
	}
}
