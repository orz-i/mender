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

type reviewAuth struct{ forbidden bool }

func (*reviewAuth) Authenticate(context.Context, string) (application.Actor, error) {
	return application.Actor{UserID: "reviewer_1"}, nil
}
func (*reviewAuth) AuthenticateMutation(_ context.Context, _ string, csrf string) (application.Actor, error) {
	if csrf != "csrf_1" {
		return application.Actor{}, application.ErrForbidden
	}
	return application.Actor{UserID: "reviewer_1"}, nil
}
func (a *reviewAuth) Authorize(context.Context, application.Actor, string, string) error {
	if a.forbidden {
		return application.ErrForbidden
	}
	return nil
}

type reviewClock struct{ at time.Time }

func (c reviewClock) Now() time.Time { return c.at }

type reviewRepo struct {
	approveCalls int
	reviewer     string
	note         string
	approveState string
}

func (*reviewRepo) ListPublicationApprovals(_ context.Context, ws string, at time.Time) ([]application.PublicationApproval, error) {
	return []application.PublicationApproval{{WorkspaceID: ws, ID: "approval_1", TargetKind: "toolset", TargetID: "set_1", TargetRevision: 9007199254740993, RequesterUserID: "maker_1", State: "pending", RequestedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour)}}, nil
}
func (r *reviewRepo) ApprovePublication(_ context.Context, ws, id, reviewer, note string, at time.Time) (application.PublicationApproval, error) {
	r.approveCalls++
	r.reviewer = reviewer
	r.note = note
	if r.approveState == "expired" {
		return application.PublicationApproval{WorkspaceID: ws, ID: id, TargetKind: "toolset", TargetID: "set_1", TargetRevision: 3, RequesterUserID: "maker_1", State: "expired", RequestedAt: at.Add(-2 * time.Hour), ExpiresAt: at.Add(-time.Hour)}, nil
	}
	return application.PublicationApproval{WorkspaceID: ws, ID: id, TargetKind: "toolset", TargetID: "set_1", TargetRevision: 3, RequesterUserID: "maker_1", ReviewerUserID: reviewer, State: "approved", RequestedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), ReviewedAt: at, DecisionNote: note}, nil
}
func (*reviewRepo) RejectPublication(context.Context, string, string, string, string, time.Time) (application.PublicationApproval, error) {
	return application.PublicationApproval{}, application.ErrUnavailable
}

func reviewRouter(t *testing.T, repo *reviewRepo, auth *reviewAuth) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service, err := application.New(repo, auth, reviewClock{at: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewPublicationReview(service, auth)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	h.Register(r)
	return r
}
func reviewRequest(r http.Handler, method, path, body, csrf string) *httptest.ResponseRecorder {
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

func TestAdminReviewListKeepsRevisionExactAndReauthorizesWorkspace(t *testing.T) {
	w := reviewRequest(reviewRouter(t, &reviewRepo{}, &reviewAuth{}), http.MethodGet, "/api/admin/v1/workspaces/ws_1/publication-approvals", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"target_revision":"9007199254740993"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = reviewRequest(reviewRouter(t, &reviewRepo{}, &reviewAuth{forbidden: true}), http.MethodGet, "/api/admin/v1/workspaces/ws_1/publication-approvals", "", "")
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestAdminApproveUsesAuthenticatedReviewerAndStrictCSRFJSON(t *testing.T) {
	repo := &reviewRepo{}
	r := reviewRouter(t, repo, &reviewAuth{})
	path := "/api/admin/v1/workspaces/ws_1/publication-approvals/approval_1/approve"
	if w := reviewRequest(r, http.MethodPost, path, `{"note":"ok"}`, ""); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if repo.approveCalls != 0 {
		t.Fatal("review reached repository without CSRF")
	}
	if w := reviewRequest(r, http.MethodPost, path, `{"note":"ok","reviewer_user_id":"maker_1"}`, "csrf_1"); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := reviewRequest(r, http.MethodPost, path, `{"note":"reviewed"}`, "csrf_1")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if repo.approveCalls != 1 || repo.reviewer != "reviewer_1" || repo.note != "reviewed" {
		t.Fatal("reviewer was not derived from authenticated session", repo.reviewer, repo.note)
	}
}

func TestAdminApproveReturnsConflictAfterServerPersistsExpiry(t *testing.T) {
	repo := &reviewRepo{approveState: "expired"}
	w := reviewRequest(reviewRouter(t, repo, &reviewAuth{}), http.MethodPost, "/api/admin/v1/workspaces/ws_1/publication-approvals/approval_1/approve", `{"note":"late"}`, "csrf_1")
	if w.Code != http.StatusConflict {
		t.Fatal(w.Code, w.Body.String())
	}
}
