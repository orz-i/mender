package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

const publicationHTTPDigest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

type publicationHTTPDigester struct{}

func (publicationHTTPDigester) SHA256([]byte) (string, error) { return publicationHTTPDigest, nil }

type publicationHTTPAuthorizer struct{ actor application.PublisherActor }

func (a *publicationHTTPAuthorizer) Authenticate(context.Context, string) (application.PublisherActor, error) {
	return a.actor, nil
}

func (a *publicationHTTPAuthorizer) AuthenticateMutation(context.Context, string, string) (application.PublisherActor, error) {
	return a.actor, nil
}

func (a *publicationHTTPAuthorizer) Authorize(context.Context, application.PublisherActor, string, string) error {
	return nil
}

type publicationHTTPRepository struct {
	createVersionCalls int
	snapshot           application.PublisherSnapshot
}

func (r *publicationHTTPRepository) Snapshot(context.Context, string) (application.PublisherSnapshot, error) {
	return r.snapshot, nil
}

func (r *publicationHTTPRepository) CreatePublisher(_ context.Context, workspace, actor, publisherID, displayName string) (application.Publisher, error) {
	now := time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC)
	return application.Publisher{WorkspaceID: workspace, ID: publisherID, OwnerUserID: actor, DisplayName: displayName, State: "active", CreatedAt: now, UpdatedAt: now}, nil
}

func (r *publicationHTTPRepository) UpdatePublisher(_ context.Context, workspace, actor, publisherID, displayName string) (application.Publisher, error) {
	return r.CreatePublisher(context.Background(), workspace, actor, publisherID, displayName)
}

func (r *publicationHTTPRepository) CreatePlugin(_ context.Context, workspace, actor, publisherID, pluginID string) (application.Plugin, error) {
	return application.Plugin{WorkspaceID: workspace, ID: pluginID, PublisherID: publisherID, CreatedByUserID: actor, CreatedAt: time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC)}, nil
}

func (r *publicationHTTPRepository) CreatePluginVersion(_ context.Context, workspace, actor string, manifest application.PluginManifest, digest string) (application.PluginVersion, error) {
	r.createVersionCalls++
	now := time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC)
	return application.PluginVersion{WorkspaceID: workspace, PluginID: manifest.PluginID, Version: manifest.Version, PublisherID: manifest.PublisherID, Revision: 1, State: "draft", Manifest: manifest, ManifestSHA256: digest, CreatedByUserID: actor, CreatedAt: now, UpdatedAt: now}, nil
}

func (r *publicationHTTPRepository) UpdatePluginVersion(_ context.Context, workspace, actor, pluginID, version string, manifest application.PluginManifest, digest string) (application.PluginVersion, error) {
	return r.CreatePluginVersion(context.Background(), workspace, actor, manifest, digest)
}

func (r *publicationHTTPRepository) PluginVersionPreflight(context.Context, string, string, string, string) (application.PluginPreflight, error) {
	return application.PluginPreflight{Ready: false, Issues: []application.PublicationIssue{{Code: "mcp_tool_unavailable", TargetID: "toolv_missing"}}}, nil
}

func publicationHTTPManifest() application.PluginManifest {
	return application.PluginManifest{
		APIVersion:   "mender.io/plugin/v1alpha1",
		PluginID:     "example.search",
		Version:      "1.0.0",
		PublisherID:  "publisher_example",
		DisplayName:  "Example search",
		Description:  "Reviewed capability references only.",
		Capabilities: []application.CapabilityReference{{Kind: "api_tool", ToolVersionID: "toolv_search_1"}},
	}
}

func publicationRouter(t *testing.T, repository *publicationHTTPRepository) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	auth := &publicationHTTPAuthorizer{actor: application.PublisherActor{UserID: "user_owner"}}
	service, err := application.NewPublisherService(repository, auth, publicationHTTPDigester{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewPublication(service, auth)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router)
	return router
}

func authenticatedPublicationRequest(method, target string, body []byte) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: publicationSessionCookie, Value: "session-token"})
	if method != http.MethodGet {
		req.Header.Set("X-Mender-CSRF", "csrf-token")
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func TestPublicationHTTPCreateVersionUsesServerFacts(t *testing.T) {
	repository := &publicationHTTPRepository{}
	router := publicationRouter(t, repository)
	body, _ := json.Marshal(publicationHTTPManifest())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, authenticatedPublicationRequest(http.MethodPost, "/api/console/v1/workspaces/ws_example/publisher/plugins/example.search/versions", body))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	if repository.createVersionCalls != 1 {
		t.Fatalf("expected one repository create, got %d", repository.createVersionCalls)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	data := response["data"].(map[string]any)
	if data["revision"] != "1" || data["state"] != "draft" {
		t.Fatalf("revision/state are not server facts: %#v", data)
	}
	if _, exists := data["created_by_user_id"]; exists {
		t.Fatal("creator user identifier leaked into Publisher projection")
	}
	if data["manifest_sha256"] != publicationHTTPDigest {
		t.Fatalf("unexpected server manifest digest: %#v", data["manifest_sha256"])
	}
}

func TestPublicationHTTPRejectsClientControlledPublicationState(t *testing.T) {
	repository := &publicationHTTPRepository{}
	router := publicationRouter(t, repository)
	body, _ := json.Marshal(publicationHTTPManifest())
	malicious := bytes.Replace(body, []byte(`"apiVersion":"mender.io/plugin/v1alpha1"`), []byte(`"apiVersion":"mender.io/plugin/v1alpha1","state":"published"`), 1)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, authenticatedPublicationRequest(http.MethodPost, "/api/console/v1/workspaces/ws_example/publisher/plugins/example.search/versions", malicious))
	if recorder.Code != http.StatusBadRequest || repository.createVersionCalls != 0 {
		t.Fatalf("client-controlled state was not rejected: status=%d calls=%d body=%s", recorder.Code, repository.createVersionCalls, recorder.Body.String())
	}
}

func TestPublicationHTTPSnapshotRedactsOwnerIdentifier(t *testing.T) {
	now := time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC)
	repository := &publicationHTTPRepository{snapshot: application.PublisherSnapshot{Publishers: []application.Publisher{{WorkspaceID: "ws_example", ID: "publisher_example", OwnerUserID: "user_owner", DisplayName: "Example", State: "active", CreatedAt: now, UpdatedAt: now}}}}
	router := publicationRouter(t, repository)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, authenticatedPublicationRequest(http.MethodGet, "/api/console/v1/workspaces/ws_example/publisher", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	response := recorder.Body.String()
	if !strings.Contains(response, `"owned":true`) || strings.Contains(response, "owner_user_id") || strings.Contains(response, "user_owner") {
		t.Fatalf("unsafe publisher owner projection: %s", response)
	}
}

func TestPublicationHTTPPreflightRequiresEmptyMutationBody(t *testing.T) {
	repository := &publicationHTTPRepository{}
	router := publicationRouter(t, repository)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, authenticatedPublicationRequest(http.MethodPost, "/api/console/v1/workspaces/ws_example/publisher/plugins/example.search/versions/1.0.0/preflight", []byte(`{}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
}
