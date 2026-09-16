package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type dangerousHTTPAuth struct{}

func (dangerousHTTPAuth) Authenticate(context.Context, string) (application.Actor, error) {
	return application.Actor{UserID: "admin_a"}, nil
}
func (dangerousHTTPAuth) AuthenticateMutation(context.Context, string, string) (application.Actor, error) {
	return application.Actor{UserID: "admin_a"}, nil
}
func (dangerousHTTPAuth) Authorize(context.Context, application.Actor, string, string) error {
	return nil
}

type dangerousHTTPIDs struct{}

func (dangerousHTTPIDs) NewDangerousOperationID() (string, error) { return "danger_http", nil }

type dangerousHTTPClock struct{}

func (dangerousHTTPClock) Now() time.Time { return time.Date(2026, 9, 16, 4, 0, 0, 0, time.UTC) }

type dangerousHTTPRepo struct{ requestCalls int }

func (r *dangerousHTTPRepo) ListDangerousOperations(context.Context, string, time.Time) ([]application.DangerousOperationApproval, error) {
	return nil, nil
}
func (r *dangerousHTTPRepo) RequestReleaseEmergency(_ context.Context, workspace, id, requester, releaseID, reason string, at, expires time.Time) (application.DangerousOperationApproval, error) {
	r.requestCalls++
	return application.DangerousOperationApproval{WorkspaceID: workspace, ID: id, RequesterUserID: requester, SubjectKind: "workspace_member", SubjectID: requester, Action: "release.emergency_disable", TargetKind: "release_plan", TargetID: releaseID, TargetVersion: "2", ParametersJSON: `{"mode":"emergency_disable"}`, ParametersSHA256: "f2f39fe20f37679b6cdc443b04b23a4d1f9e943a7d5835e2724fdd0522c56adc", Reason: reason, State: "pending", RequestedAt: at, ExpiresAt: expires}, nil
}
func (r *dangerousHTTPRepo) ApproveDangerousOperation(context.Context, string, string, string, time.Time, string) (application.DangerousOperationApproval, error) {
	return application.DangerousOperationApproval{}, nil
}
func (r *dangerousHTTPRepo) RejectDangerousOperation(context.Context, string, string, string, time.Time, string) (application.DangerousOperationApproval, error) {
	return application.DangerousOperationApproval{}, nil
}

func TestDangerousOperationHTTPRejectsClientAuthorityFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &dangerousHTTPRepo{}
	auth := dangerousHTTPAuth{}
	service, _ := application.NewDangerousOperationService(repo, auth, dangerousHTTPIDs{}, dangerousHTTPClock{})
	handler, _ := NewDangerousOperation(service, auth)
	router := gin.New()
	handler.Register(router)
	body := []byte(`{"release_id":"release_a","reason":"incident","ttl_seconds":600,"approved":true,"target_version":"99","parameters_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","actor_user_id":"attacker"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/workspaces/ws_a/dangerous-operations/release-emergency-requests", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session"})
	req.Header.Set("X-Mender-CSRF", "csrf")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || repo.requestCalls != 0 {
		t.Fatal(rec.Code, repo.requestCalls, rec.Body.String())
	}
}

func TestDangerousOperationHTTPRequiresCSRF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &dangerousHTTPRepo{}
	auth := dangerousHTTPAuth{}
	service, _ := application.NewDangerousOperationService(repo, auth, dangerousHTTPIDs{}, dangerousHTTPClock{})
	handler, _ := NewDangerousOperation(service, auth)
	router := gin.New()
	handler.Register(router)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/workspaces/ws_a/dangerous-operations/release-emergency-requests", bytes.NewBufferString(`{"release_id":"release_a","reason":"incident","ttl_seconds":600}`))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || repo.requestCalls != 0 {
		t.Fatal(rec.Code, repo.requestCalls)
	}
}
