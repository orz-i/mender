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

type supportHTTPAuth struct{}
func (supportHTTPAuth) Authenticate(context.Context, string) (application.Actor, error) { return application.Actor{UserID: "staff_a"}, nil }
func (supportHTTPAuth) AuthenticateMutation(context.Context, string, string) (application.Actor, error) { return application.Actor{UserID: "staff_a"}, nil }
func (supportHTTPAuth) AuthorizePlatform(context.Context, application.Actor, string) error { return nil }
type supportHTTPIDs struct{}
func (supportHTTPIDs) NewDangerousOperationID() (string, error) { return "danger_http_support", nil }
func (supportHTTPIDs) NewJITGrantID() (string, error) { return "jit_http_support", nil }
type supportHTTPClock struct{}
func (supportHTTPClock) Now() time.Time { return time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC) }
type supportHTTPManagement struct{ requestCalls, activateCalls int }
func (r *supportHTTPManagement) RequestSupportJIT(_ context.Context, workspace, id, requester string, _ []string, _ time.Duration, reason string, at, expires time.Time) (application.DangerousOperationApproval, error) {
	r.requestCalls++
	return application.DangerousOperationApproval{WorkspaceID: workspace, ID: id, RequesterUserID: requester, SubjectKind: "platform_staff", SubjectID: requester, Action: "support.workspace_read", TargetKind: "workspace", TargetID: workspace, ParametersJSON: `{"scopes":["run:read"],"ttl_seconds":600}`, ParametersSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Reason: reason, State: "pending", RequestedAt: at, ExpiresAt: expires}, nil
}
func (*supportHTTPManagement) ApproveDangerousOperation(context.Context, string, string, string, time.Time, string) (application.DangerousOperationApproval, error) { return application.DangerousOperationApproval{}, nil }
func (*supportHTTPManagement) RejectDangerousOperation(context.Context, string, string, string, time.Time, string) (application.DangerousOperationApproval, error) { return application.DangerousOperationApproval{}, nil }
func (r *supportHTTPManagement) ActivateJITSupport(_ context.Context, workspace, approvalID, grantID, userID string, at time.Time) (application.JITSupportGrant, error) {
	r.activateCalls++
	return application.JITSupportGrant{WorkspaceID: workspace, ID: grantID, ApprovalID: approvalID, UserID: userID, Reason: "incident", Scopes: []string{"run:read"}, CreatedAt: at, ExpiresAt: at.Add(10*time.Minute)}, nil
}
func (*supportHTTPManagement) RevokeJITSupport(context.Context, string, string, string, time.Time, string) (application.JITSupportGrant, error) { return application.JITSupportGrant{}, nil }
type supportHTTPRead struct{}
func (supportHTTPRead) ListSupportRuns(context.Context, string, string, time.Time) ([]application.SupportRun, error) { return nil, nil }

func supportHTTPRouter(t *testing.T, management *supportHTTPManagement) *gin.Engine {
	t.Helper(); gin.SetMode(gin.TestMode)
	auth := supportHTTPAuth{}
	service, err := application.NewSupportAccessService(management, supportHTTPRead{}, auth, supportHTTPIDs{}, supportHTTPClock{})
	if err != nil { t.Fatal(err) }
	handler, err := NewSupportAccess(service, auth); if err != nil { t.Fatal(err) }
	router := gin.New(); handler.Register(router); return router
}

func TestSupportHTTPRejectsClientAuthorityFields(t *testing.T) {
	management := &supportHTTPManagement{}; router := supportHTTPRouter(t, management)
	body := []byte(`{"scopes":["run:read"],"ttl_seconds":600,"reason":"incident","approval_id":"fake","parameters_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expires_at":"2099-01-01T00:00:00Z"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/support/workspaces/ws_a/jit-requests", bytes.NewReader(body)); req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session"}); req.Header.Set("X-Mender-CSRF", "csrf")
	rec := httptest.NewRecorder(); router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || management.requestCalls != 0 { t.Fatal(rec.Code, management.requestCalls, rec.Body.String()) }
}

func TestSupportActivationCannotResubmitScopesOrTTL(t *testing.T) {
	management := &supportHTTPManagement{}; router := supportHTTPRouter(t, management)
	body := []byte(`{"scopes":["run:read"],"ttl_seconds":3600}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/support/workspaces/ws_a/jit-requests/danger_a/activate", bytes.NewReader(body)); req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session"}); req.Header.Set("X-Mender-CSRF", "csrf")
	rec := httptest.NewRecorder(); router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || management.activateCalls != 0 { t.Fatal(rec.Code, management.activateCalls, rec.Body.String()) }
}
