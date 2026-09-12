package consolelaunch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	launchhttp "github.com/orz-i/mender/backend/internal/processes/consolelaunch/adapters/inbound/httpapi"
	launchapp "github.com/orz-i/mender/backend/internal/processes/consolelaunch/application"
)

type auth struct{}

func (auth) Authenticate(_ context.Context, raw string) (launchapp.Actor, error) {
	if raw != "session-alpha" {
		return launchapp.Actor{}, launchapp.ErrUnauthenticated
	}
	return launchapp.Actor{UserID: "user_alpha"}, nil
}
func (auth) Authorize(_ context.Context, actor launchapp.Actor, workspace, action string) error {
	if actor.UserID != "user_alpha" || workspace != "ws_alpha" || action != "workspace:read" {
		return launchapp.ErrForbidden
	}
	return nil
}

type repo struct{}

func (repo) ListLaunchOptions(context.Context, string, string, time.Time) ([]launchapp.LaunchOption, error) {
	return []launchapp.LaunchOption{{WorkspaceID: "ws_alpha", ToolsetVersionID: "set_alpha_v1", ToolID: "tool_search", ToolVersion: "1.0.0", ToolVersionID: "tool_search_v1", Title: "Search", Description: "Reviewed", InputSchema: `{"type":"object","properties":{"query":{"type":"string"}}}`, SideEffect: "read_only", Idempotency: "safe_read", ConnectionID: "conn_alpha", ProviderID: "provider_alpha", Currency: "USD", ReserveMicro: 60}}, nil
}

type clock struct{ at time.Time }

func (c clock) Now() time.Time { return c.at }

func TestHumanLaunchDiscoveryReturnsOnlySafeProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, err := launchapp.New(auth{}, repo{}, clock{time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	h, err := launchhttp.New(service)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	h.Register(router)
	req := httptest.NewRequest(http.MethodGet, "/api/console/v1/workspaces/ws_alpha/launch-options", nil)
	req.AddCookie(&http.Cookie{Name: "mender_session", Value: "session-alpha"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, forbidden := range []string{"credential_version_ref", "budget_id", "period_id", "remaining_micro"} {
		if strings.Contains(body, forbidden) {
			t.Fatal("launch response leaked protected field", forbidden, body)
		}
	}
	if !strings.Contains(body, `"reserve_micro":"60"`) || !strings.Contains(body, `"connection_id":"conn_alpha"`) {
		t.Fatal("safe launch projection missing", body)
	}
}
