package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type callbackInboxRepo struct{ filter application.ProviderCallbackInboxFilter }

func (r *callbackInboxRepo) ListProviderCallbackInbox(_ context.Context, workspace string, filter application.ProviderCallbackInboxFilter) (application.ProviderCallbackInboxPage, error) {
	r.filter = filter
	at := time.Date(2026, 9, 14, 6, 30, 0, 0, time.UTC)
	return application.ProviderCallbackInboxPage{
		Items: []application.ProviderCallbackInboxItem{{
			ReceiptID: strings.Repeat("a", 64), WorkspaceID: workspace, ProviderID: "provider_agent", EventID: "evt.agent.1",
			RunID: "run_agent", ObservationID: "obs.agent.1", ObservationState: "succeeded", Disposition: "accepted",
			DeliveryCount: 3, DuplicateDeliveryCount: 2, ReceivedAt: at, LastReceivedAt: at.Add(time.Second), ObservedAt: at.Add(-time.Second), ProcessedAt: at,
		}},
		NextBeforeReceivedAt: at, NextBeforeReceiptID: strings.Repeat("a", 64),
	}, nil
}

func callbackInboxRouter(t *testing.T, repo *callbackInboxRepo) *gin.Engine {
	t.Helper()
	auth := &reviewAuth{}
	service, err := application.NewProviderCallbackInbox(repo, auth)
	if err != nil { t.Fatal(err) }
	handler, err := NewProviderCallbackInbox(service, auth)
	if err != nil { t.Fatal(err) }
	r := gin.New(); handler.Register(r); return r
}

func TestProviderCallbackInboxReturnsOnlySafeServerFactsAndFilters(t *testing.T) {
	repo := &callbackInboxRepo{}
	r := callbackInboxRouter(t, repo)
	w := reviewRequest(r, http.MethodGet, "/api/admin/v1/workspaces/ws_1/provider-callbacks?provider_id=provider_agent&disposition=accepted&limit=50", "", "")
	if w.Code != http.StatusOK || repo.filter.ProviderID != "provider_agent" || repo.filter.Disposition != "accepted" || repo.filter.Limit != 50 {
		t.Fatal(w.Code, repo.filter, w.Body.String())
	}
	body := w.Body.String()
	for _, forbidden := range []string{"body_sha256", "key_id", "signature", "provider_request_id", "external_task_id", "result_json", "error_code"} {
		if strings.Contains(body, forbidden) { t.Fatal("callback Admin projection leaked sensitive field", forbidden, body) }
	}
	for _, required := range []string{`"provider_id":"provider_agent"`, `"delivery_count":3`, `"duplicate_delivery_count":2`, `"disposition":"accepted"`, `"observation_state":"succeeded"`} {
		if !strings.Contains(body, required) { t.Fatal("missing callback Admin fact", required, body) }
	}
}

func TestProviderCallbackInboxRejectsUnknownFiltersAndIncompleteCursor(t *testing.T) {
	r := callbackInboxRouter(t, &callbackInboxRepo{})
	for _, path := range []string{
		"/api/admin/v1/workspaces/ws_1/provider-callbacks?body_sha256=secret",
		"/api/admin/v1/workspaces/ws_1/provider-callbacks?before_received_at=2026-09-14T06:30:00Z",
		"/api/admin/v1/workspaces/ws_1/provider-callbacks?limit=101",
	} {
		if w := reviewRequest(r, http.MethodGet, path, "", ""); w.Code != http.StatusBadRequest { t.Fatal(path, w.Code, w.Body.String()) }
	}
}
