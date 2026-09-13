package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type historyAuth struct {
	forbidden bool
	action    string
}

func (*historyAuth) Authenticate(context.Context, string) (application.Actor, error) {
	return application.Actor{UserID: "auditor_1"}, nil
}
func (*historyAuth) AuthenticateMutation(context.Context, string, string) (application.Actor, error) {
	return application.Actor{}, application.ErrForbidden
}
func (a *historyAuth) Authorize(_ context.Context, _ application.Actor, _ string, action string) error {
	a.action = action
	if a.forbidden || action != "catalog:audit" {
		return application.ErrForbidden
	}
	return nil
}

type historyRepo struct {
	filter application.PublicationHistoryFilter
	page   application.PublicationHistoryPage
	calls  int
}

func (r *historyRepo) ListPublicationHistory(_ context.Context, _ string, filter application.PublicationHistoryFilter) (application.PublicationHistoryPage, error) {
	r.calls++
	r.filter = filter
	return r.page, nil
}

func historyRouter(t *testing.T, repo *historyRepo, auth *historyAuth) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service, err := application.NewHistory(repo, auth)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewPublicationHistory(service, auth)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	handler.Register(r)
	return r
}

func historyRequest(r http.Handler, path string, withCookie bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if withCookie {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "session_1"})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAdminPublicationHistoryUsesBoundedExactCursorAndServerAuthorization(t *testing.T) {
	at := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	repo := &historyRepo{page: application.PublicationHistoryPage{
		Events: []application.PublicationAuditEvent{
			{Sequence: 9007199254740993, WorkspaceID: "ws_1", ApprovalID: "approval_1", TargetKind: "tool_version", TargetID: "tv_1", TargetRevision: 9007199254740991, ObservedRevision: 9007199254740992, EventKind: "approval_expired", OccurredAt: at, ReasonCode: "revision_drift"},
			{Sequence: 9007199254740992, WorkspaceID: "ws_1", ApprovalID: "approval_1", TargetKind: "tool_version", TargetID: "tv_1", TargetRevision: 9007199254740991, ObservedRevision: 9007199254740991, EventKind: "approval_approved", ActorUserID: "reviewer_1", OccurredAt: at.Add(-time.Second)},
		},
		NextBeforeSequence: 9007199254740992,
	}}
	auth := &historyAuth{}
	path := "/api/admin/v1/workspaces/ws_1/publication-history?target_kind=tool_version&target_id=tv_1&approval_id=approval_1&limit=2"
	w := historyRequest(historyRouter(t, repo, auth), path, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, expected := range []string{`"sequence":"9007199254740993"`, `"target_revision":"9007199254740991"`, `"observed_revision":"9007199254740992"`, `"next_before_sequence":"9007199254740992"`} {
		if !strings.Contains(body, expected) {
			t.Fatal("history lost exact integer string", expected, body)
		}
	}
	if repo.calls != 1 || repo.filter.TargetKind != "tool_version" || repo.filter.TargetID != "tv_1" || repo.filter.ApprovalID != "approval_1" || repo.filter.Limit != 2 || auth.action != "catalog:audit" {
		t.Fatal("history query did not preserve strict server filter/authorization", repo.filter, auth.action)
	}
}

func TestAdminPublicationHistoryRejectsUnsafeQueryAndWorkspaceDrift(t *testing.T) {
	repo := &historyRepo{}
	r := historyRouter(t, repo, &historyAuth{})
	for _, path := range []string{
		"/api/admin/v1/workspaces/ws_1/publication-history?unknown=x",
		"/api/admin/v1/workspaces/ws_1/publication-history?target_id=a&target_id=b",
		"/api/admin/v1/workspaces/ws_1/publication-history?before_sequence=0",
		"/api/admin/v1/workspaces/ws_1/publication-history?limit=101",
	} {
		if w := historyRequest(r, path, true); w.Code != 400 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	if repo.calls != 0 {
		t.Fatal("invalid history query reached repository")
	}
	if w := historyRequest(r, "/api/admin/v1/workspaces/ws_1/publication-history", false); w.Code != 401 {
		t.Fatal(w.Code, w.Body.String())
	}

	at := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	drift := &historyRepo{page: application.PublicationHistoryPage{Events: []application.PublicationAuditEvent{{
		Sequence: 1, WorkspaceID: "ws_other", ApprovalID: "approval_1", TargetKind: "toolset", TargetID: "set_1",
		TargetRevision: 1, ObservedRevision: 1, EventKind: "approval_submitted", ActorUserID: "maker_1", OccurredAt: at,
	}}}}
	w := historyRequest(historyRouter(t, drift, &historyAuth{}), "/api/admin/v1/workspaces/ws_1/publication-history", true)
	if w.Code != 503 {
		t.Fatal("workspace drift was not rejected", w.Code, w.Body.String())
	}
}
