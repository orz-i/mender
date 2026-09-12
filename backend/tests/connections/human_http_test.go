package connections_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	connectionhttp "github.com/orz-i/mender/backend/internal/contexts/connections/adapters/inbound/httpapi"
	connectionapp "github.com/orz-i/mender/backend/internal/contexts/connections/application"
	connectiondomain "github.com/orz-i/mender/backend/internal/contexts/connections/domain"
)

type humanAuthorizer struct{ manage bool }

func (a humanAuthorizer) Authenticate(_ context.Context, raw string) (connectionapp.HumanActor, error) {
	if raw != "session-alpha" {
		return connectionapp.HumanActor{}, connectionapp.ErrUnauthenticated
	}
	return connectionapp.HumanActor{UserID: "user_alpha"}, nil
}
func (a humanAuthorizer) AuthenticateMutation(ctx context.Context, raw, csrf string) (connectionapp.HumanActor, error) {
	actor, err := a.Authenticate(ctx, raw)
	if err != nil {
		return connectionapp.HumanActor{}, err
	}
	if csrf != "csrf-alpha" {
		return connectionapp.HumanActor{}, connectionapp.ErrForbidden
	}
	return actor, nil
}
func (a humanAuthorizer) Authorize(_ context.Context, actor connectionapp.HumanActor, workspace, action string) error {
	if actor.UserID != "user_alpha" || workspace != "ws_alpha" {
		return connectionapp.ErrForbidden
	}
	if action == "connection:read" {
		return nil
	}
	if action == "connection:manage" && a.manage {
		return nil
	}
	return connectionapp.ErrForbidden
}

type humanRepo struct{ item connectiondomain.Summary }

func (r *humanRepo) ListSummaries(context.Context, string) ([]connectiondomain.Summary, error) {
	return []connectiondomain.Summary{r.item}, nil
}
func (r *humanRepo) Revoke(_ context.Context, _, _ string, _ time.Time) (connectiondomain.Summary, error) {
	r.item.State = "revoked"
	r.item.Revision++
	return r.item, nil
}

func TestHumanConnectionsHTTPRequiresSessionAndCSRF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := time.Now().UTC().Add(-time.Hour)
	repo := &humanRepo{item: connectiondomain.Summary{WorkspaceID: "ws_alpha", ConnectionID: "conn_alpha", ProviderID: "provider_alpha", State: "active", Revision: 1, CreatedAt: base, ExpiresAt: base.Add(2 * time.Hour)}}
	auth := humanAuthorizer{manage: true}
	service, err := connectionapp.NewHuman(repo, auth)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := connectionhttp.New(service, auth)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router)

	unauthenticated := httptest.NewRecorder()
	router.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/console/v1/workspaces/ws_alpha/connections", nil))
	if unauthenticated.Code != 401 {
		t.Fatal("anonymous Connection list accepted", unauthenticated.Code)
	}

	listRequest := httptest.NewRequest(http.MethodGet, "/api/console/v1/workspaces/ws_alpha/connections", nil)
	listRequest.AddCookie(&http.Cookie{Name: "mender_session", Value: "session-alpha"})
	list := httptest.NewRecorder()
	router.ServeHTTP(list, listRequest)
	if list.Code != 200 || strings.Contains(list.Body.String(), "credential") || !strings.Contains(list.Body.String(), "conn_alpha") {
		t.Fatal("unsafe Connection list", list.Code, list.Body.String())
	}

	noCSRF := httptest.NewRequest(http.MethodDelete, "/api/console/v1/workspaces/ws_alpha/connections/conn_alpha", nil)
	noCSRF.AddCookie(&http.Cookie{Name: "mender_session", Value: "session-alpha"})
	noCSRFResult := httptest.NewRecorder()
	router.ServeHTTP(noCSRFResult, noCSRF)
	if noCSRFResult.Code != 403 {
		t.Fatal("Connection revoke without CSRF accepted", noCSRFResult.Code)
	}

	revoke := httptest.NewRequest(http.MethodDelete, "/api/console/v1/workspaces/ws_alpha/connections/conn_alpha", nil)
	revoke.AddCookie(&http.Cookie{Name: "mender_session", Value: "session-alpha"})
	revoke.Header.Set("X-Mender-CSRF", "csrf-alpha")
	revokeResult := httptest.NewRecorder()
	router.ServeHTTP(revokeResult, revoke)
	if revokeResult.Code != 200 || !strings.Contains(revokeResult.Body.String(), `"state":"revoked"`) {
		t.Fatal("Connection revoke failed", revokeResult.Code, revokeResult.Body.String())
	}
}

func TestHumanConnectionsHTTPViewerCannotRevoke(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := time.Now().UTC().Add(-time.Hour)
	repo := &humanRepo{item: connectiondomain.Summary{WorkspaceID: "ws_alpha", ConnectionID: "conn_alpha", ProviderID: "provider_alpha", State: "active", Revision: 1, CreatedAt: base, ExpiresAt: base.Add(2 * time.Hour)}}
	auth := humanAuthorizer{manage: false}
	service, _ := connectionapp.NewHuman(repo, auth)
	handler, _ := connectionhttp.New(service, auth)
	router := gin.New()
	handler.Register(router)
	revoke := httptest.NewRequest(http.MethodDelete, "/api/console/v1/workspaces/ws_alpha/connections/conn_alpha", nil)
	revoke.AddCookie(&http.Cookie{Name: "mender_session", Value: "session-alpha"})
	revoke.Header.Set("X-Mender-CSRF", "csrf-alpha")
	result := httptest.NewRecorder()
	router.ServeHTTP(result, revoke)
	if result.Code != 403 || repo.item.State != "active" {
		t.Fatal("viewer revoked Connection", result.Code, repo.item.State)
	}
}
