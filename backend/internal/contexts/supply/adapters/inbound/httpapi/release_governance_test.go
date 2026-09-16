package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

type releaseHTTPAuth struct{}

func (releaseHTTPAuth) Authenticate(context.Context, string) (application.PublisherActor, error) {
	return application.PublisherActor{UserID: "admin_release"}, nil
}
func (releaseHTTPAuth) AuthenticateMutation(context.Context, string, string) (application.PublisherActor, error) {
	return application.PublisherActor{UserID: "admin_release"}, nil
}
func (releaseHTTPAuth) Authorize(context.Context, application.PublisherActor, string, string) error {
	return nil
}

type releaseHTTPID struct{}

func (releaseHTTPID) NewID() (string, error) { return "release_test", nil }

type releaseHTTPClock struct{}

func (releaseHTTPClock) Now() time.Time { return time.Date(2026, 9, 16, 2, 0, 0, 0, time.UTC) }

type releaseHTTPRepo struct{ createCalls int }

func (r *releaseHTTPRepo) ReleaseSnapshot(context.Context, string) (application.ReleaseSnapshot, error) {
	return application.ReleaseSnapshot{}, nil
}
func (r *releaseHTTPRepo) CreateReleasePlan(_ context.Context, workspace, id, actor string, in application.ReleasePlanInput, at time.Time) (application.ReleasePlan, error) {
	r.createCalls++
	return application.ReleasePlan{WorkspaceID: workspace, ID: id, PluginID: in.PluginID, PluginVersion: in.PluginVersion, ToolsetVersionID: in.ToolsetVersionID, ToolVersionID: in.ToolVersionID, ProviderID: in.ProviderID, StableDeploymentRevision: in.StableDeploymentRevision, CandidateDeploymentRevision: in.CandidateDeploymentRevision, Revision: 1, State: "draft", CreatedByUserID: actor, CreatedAt: at, UpdatedAt: at}, nil
}
func (r *releaseHTTPRepo) StartReleaseCanary(context.Context, string, string, string, string, time.Time, time.Time) (application.ReleasePlan, error) {
	return application.ReleasePlan{}, nil
}
func (r *releaseHTTPRepo) PromoteRelease(context.Context, string, string, string, string, time.Time) (application.ReleasePlan, error) {
	return application.ReleasePlan{}, nil
}
func (r *releaseHTTPRepo) DrainRelease(context.Context, string, string, string, string, time.Time) (application.ReleasePlan, error) {
	return application.ReleasePlan{}, nil
}
func (r *releaseHTTPRepo) RollbackRelease(context.Context, string, string, string, string, time.Time) (application.ReleasePlan, error) {
	return application.ReleasePlan{}, nil
}
func (r *releaseHTTPRepo) EmergencyDisableRelease(context.Context, string, string, string, string, time.Time) (application.ReleasePlan, error) {
	return application.ReleasePlan{}, nil
}

func TestReleaseGovernanceHTTPRejectsClientControlledState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &releaseHTTPRepo{}
	auth := releaseHTTPAuth{}
	service, err := application.NewReleaseGovernance(repo, auth, releaseHTTPID{}, releaseHTTPClock{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewReleaseGovernance(service, auth)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router)
	body := []byte(`{"plugin_id":"example.search","plugin_version":"2.0.0","toolset_version_id":"set_release","tool_version_id":"tv_release","provider_id":"provider_release","stable_deployment_revision":"deploy_stable","candidate_deployment_revision":"deploy_candidate","reason":"prepare canary","state":"active","revision":"99","actor_user_id":"attacker"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/workspaces/ws_release/releases/plans", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: publicationSessionCookie, Value: "session"})
	req.Header.Set("X-Mender-CSRF", "csrf")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || repo.createCalls != 0 {
		t.Fatalf("client controlled release facts reached service: status=%d calls=%d body=%s", rec.Code, repo.createCalls, rec.Body.String())
	}
}

func TestReleaseGovernanceHTTPRequiresCSRF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &releaseHTTPRepo{}
	auth := releaseHTTPAuth{}
	service, _ := application.NewReleaseGovernance(repo, auth, releaseHTTPID{}, releaseHTTPClock{})
	handler, _ := NewReleaseGovernance(service, auth)
	router := gin.New()
	handler.Register(router)
	body := []byte(`{"plugin_id":"example.search","plugin_version":"2.0.0","toolset_version_id":"set_release","tool_version_id":"tv_release","provider_id":"provider_release","stable_deployment_revision":"deploy_stable","candidate_deployment_revision":"deploy_candidate","reason":"prepare canary"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/workspaces/ws_release/releases/plans", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: publicationSessionCookie, Value: "session"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || repo.createCalls != 0 {
		t.Fatalf("release mutation bypassed CSRF: status=%d calls=%d", rec.Code, repo.createCalls)
	}
}
