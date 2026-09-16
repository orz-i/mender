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

type platformHTTPAuth struct{}

func (platformHTTPAuth) Authenticate(context.Context, string) (application.Actor, error) {
	return application.Actor{UserID: "operator_a"}, nil
}
func (platformHTTPAuth) AuthenticateMutation(context.Context, string, string) (application.Actor, error) {
	return application.Actor{UserID: "operator_a"}, nil
}
func (platformHTTPAuth) AuthorizePlatform(context.Context, application.Actor, string) error {
	return nil
}

type platformHTTPIDs struct{}

func (platformHTTPIDs) NewPlatformIncidentID() (string, error) { return "incident_test", nil }

type platformHTTPClock struct{}

func (platformHTTPClock) Now() time.Time { return time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC) }

type platformHTTPRepo struct{ workspaceCalls, incidentCalls int }

func (r *platformHTTPRepo) ListPlatformWorkspaces(context.Context, string) ([]application.PlatformWorkspace, error) {
	return nil, nil
}
func (r *platformHTTPRepo) SetPlatformWorkspaceFrozen(_ context.Context, workspace string, expected int64, frozen bool, actor, reason string, at time.Time) (application.PlatformWorkspace, error) {
	r.workspaceCalls++
	return application.PlatformWorkspace{WorkspaceID: workspace, Frozen: frozen, Revision: expected + 1, Reason: reason, ActorUserID: actor, UpdatedAt: at, CreatedAt: at.Add(-time.Hour)}, nil
}
func (r *platformHTTPRepo) ListPlatformProviders(context.Context, string) ([]application.PlatformProvider, error) {
	return nil, nil
}
func (r *platformHTTPRepo) SetPlatformProviderState(context.Context, string, int64, string, string, string, time.Time) (application.PlatformProvider, error) {
	return application.PlatformProvider{}, nil
}
func (r *platformHTTPRepo) OpenPlatformIncident(_ context.Context, id, targetKind, targetID, severity, code, actor, reason string, at time.Time) (application.PlatformIncident, error) {
	r.incidentCalls++
	return application.PlatformIncident{ID: id, TargetKind: targetKind, TargetID: targetID, Severity: severity, Code: code, State: "open", OpenedByUserID: actor, OpenReason: reason, Revision: 1, OpenedAt: at, UpdatedAt: at}, nil
}
func (r *platformHTTPRepo) ResolvePlatformIncident(context.Context, string, int64, string, string, time.Time) (application.PlatformIncident, error) {
	return application.PlatformIncident{}, nil
}
func (r *platformHTTPRepo) ListPlatformIncidents(context.Context, string, application.PlatformIncidentFilter) ([]application.PlatformIncident, error) {
	return nil, nil
}
func (r *platformHTTPRepo) ListPlatformAudit(context.Context, string, application.PlatformAuditFilter) ([]application.PlatformAdminAuditEvent, error) {
	return nil, nil
}

func platformHTTPRouter(t *testing.T, repo *platformHTTPRepo) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	auth := platformHTTPAuth{}
	service, err := application.NewPlatformAdminService(repo, auth, platformHTTPIDs{}, platformHTTPClock{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewPlatformAdmin(service, auth)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router)
	return router
}

func TestPlatformAdminMutationRejectsClientOwnedResultFacts(t *testing.T) {
	repo := &platformHTTPRepo{}
	router := platformHTTPRouter(t, repo)
	body := []byte(`{"expected_revision":"1","reason":"incident","frozen":true,"actor_user_id":"attacker","revision":"99"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/platform/workspaces/ws_a/freeze", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session"})
	req.Header.Set("X-Mender-CSRF", "csrf")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || repo.workspaceCalls != 0 {
		t.Fatal(rec.Code, repo.workspaceCalls, rec.Body.String())
	}
}

func TestPlatformAdminMutationRequiresCSRF(t *testing.T) {
	repo := &platformHTTPRepo{}
	router := platformHTTPRouter(t, repo)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/platform/workspaces/ws_a/freeze", bytes.NewBufferString(`{"expected_revision":"1","reason":"incident"}`))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || repo.workspaceCalls != 0 {
		t.Fatal(rec.Code, repo.workspaceCalls, rec.Body.String())
	}
}

func TestPlatformIncidentIDAndActorCannotBeSubmitted(t *testing.T) {
	repo := &platformHTTPRepo{}
	router := platformHTTPRouter(t, repo)
	body := []byte(`{"target_kind":"provider","target_id":"provider_a","severity":"critical","code":"provider.anomaly","reason":"incident","id":"attacker","opened_by_user_id":"attacker"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/platform/incidents", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session"})
	req.Header.Set("X-Mender-CSRF", "csrf")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || repo.incidentCalls != 0 {
		t.Fatal(rec.Code, repo.incidentCalls, rec.Body.String())
	}
}
