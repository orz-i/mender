package commerce_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	commercehttp "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/inbound/httpapi"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
)

type usageAuthorizer struct {
	actor application.HumanUsageActor
	err   error
}

func TestRunCostUsesExplicitRunDelegationAndPersistedQuotaFacts(t *testing.T) {
	fixture := usageFixture()
	repo := &usageRepository{runCost: fixture.Entries[1]}
	auth := &runCostAuthorizer{actor: application.RunCostActor{WorkspaceID: "ws_a", SubjectID: "user_a", CredentialID: "rd_alpha"}}
	service, err := application.NewRunCostService(repo, auth)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := commercehttp.NewRunCost(service, auth)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler.RegisterAt(router, "/api/console/v1")
	request := func(token, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		// A browser cookie is intentionally irrelevant to this endpoint.
		r.AddCookie(&http.Cookie{Name: "mender_session", Value: "browser-session-must-not-authorize-run-cost"})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	base := "/api/console/v1/workspaces/ws_a/runs/run_done/cost"
	if w := request("", base); w.Code != http.StatusUnauthorized || repo.calls != 0 {
		t.Fatal(w.Code, w.Body.String(), repo.calls)
	}
	if w := request("delegated_alpha", "/api/console/v1/workspaces/ws_b/runs/run_done/cost"); w.Code != http.StatusForbidden || repo.calls != 0 {
		t.Fatal(w.Code, w.Body.String(), repo.calls)
	}
	w := request("delegated_alpha", base)
	if w.Code != http.StatusOK || repo.calls != 1 {
		t.Fatal(w.Code, w.Body.String(), repo.calls)
	}
	body := w.Body.String()
	for _, want := range []string{`"run_id":"run_done"`, `"quota_state":"settled"`, `"reserved_micro":"200"`, `"charged_micro":"125"`, `"released_micro":"75"`, `"accounting_scope":"quota_only"`} {
		if !strings.Contains(body, want) {
			t.Fatal("missing Run quota fact", want, body)
		}
	}
	for _, forbidden := range []string{"reservation_id", "price_version_id", "settlement_job", "provider_request_id", "browser-session-must-not-authorize"} {
		if strings.Contains(body, forbidden) {
			t.Fatal("Run cost response leaked internal value", forbidden, body)
		}
	}
	if w = request("delegated_alpha", "/api/console/v1/workspaces/ws_a/runs/run_missing/cost"); w.Code != http.StatusForbidden || repo.calls != 1 {
		// The authorizer rejects a different Run target before Commerce storage.
		t.Fatal(w.Code, w.Body.String(), repo.calls)
	}
}

func (r *usageRepository) RunCost(_ context.Context, workspace, runID string) (application.UsageEntryView, error) {
	r.calls++
	if workspace != "ws_a" || runID != r.runCost.RunID {
		return application.UsageEntryView{}, application.ErrObservabilityNotFound
	}
	return r.runCost, nil
}

type runCostAuthorizer struct {
	actor application.RunCostActor
	err   error
}

func (a *runCostAuthorizer) AuthenticateRunCost(_ context.Context, token string) (application.RunCostActor, error) {
	if token != "delegated_alpha" {
		return application.RunCostActor{}, application.ErrObservabilityUnauthenticated
	}
	if a.err != nil {
		return application.RunCostActor{}, a.err
	}
	return a.actor, nil
}

func (a *runCostAuthorizer) AuthorizeRunCost(_ context.Context, actor application.RunCostActor, workspace, runID, action string) error {
	if a.err != nil {
		return a.err
	}
	if actor != a.actor || workspace != "ws_a" || runID != "run_done" || action != "run:read" {
		return application.ErrObservabilityForbidden
	}
	return nil
}

func (a *usageAuthorizer) Authenticate(context.Context, string) (application.HumanUsageActor, error) {
	if a.err != nil {
		return application.HumanUsageActor{}, a.err
	}
	return a.actor, nil
}
func (a *usageAuthorizer) Authorize(_ context.Context, actor application.HumanUsageActor, workspace, action string) error {
	if a.err != nil {
		return a.err
	}
	if actor != a.actor || workspace != "ws_a" || action != "usage:read" {
		return application.ErrObservabilityForbidden
	}
	return nil
}

type usageRepository struct {
	snapshot application.UsageSnapshot
	runCost  application.UsageEntryView
	calls    int
}

func (r *usageRepository) Snapshot(_ context.Context, workspace string, limit int) (application.UsageSnapshot, error) {
	r.calls++
	if workspace != "ws_a" || limit != 100 {
		return application.UsageSnapshot{}, application.ErrObservabilityUnavailable
	}
	return r.snapshot, nil
}

func usageFixture() application.UsageSnapshot {
	at := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	charged := int64(125)
	return application.UsageSnapshot{
		BudgetPeriods: []application.BudgetPeriodView{{
			WorkspaceID: "ws_a", BudgetID: "budget_a", PeriodID: "period_a", Currency: "USD",
			StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour), Active: true,
			LimitMicro: 1000, ConsumedMicro: 125, ReservedMicro: 75, Revision: 4,
		}},
		Entries: []application.UsageEntryView{
			{WorkspaceID: "ws_a", RunID: "run_held", BudgetID: "budget_a", PeriodID: "period_a", Currency: "USD", QuotaState: "held", ReservedMicro: 75, CreatedAt: at},
			{WorkspaceID: "ws_a", RunID: "run_done", BudgetID: "budget_a", PeriodID: "period_a", Currency: "USD", QuotaState: "settled", ReservedMicro: 200, ChargedMicro: &charged, Outcome: "succeeded", CreatedAt: at.Add(-time.Minute), FinalizedAt: at},
		},
	}
}

func setupUsage(t *testing.T, repo *usageRepository, auth *usageAuthorizer) http.Handler {
	t.Helper()
	service, err := application.NewUsageService(repo, auth)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := commercehttp.NewUsage(service, auth)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler.Register(router)
	return router
}

func requestUsage(h http.Handler, cookie, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: "mender_session", Value: cookie})
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestUsageObservabilityRequiresHumanMembershipAndKeepsMicroAmountsExact(t *testing.T) {
	repo := &usageRepository{snapshot: usageFixture()}
	auth := &usageAuthorizer{actor: application.HumanUsageActor{UserID: "user_a"}}
	h := setupUsage(t, repo, auth)

	if w := requestUsage(h, "", "/api/console/v1/workspaces/ws_a/usage"); w.Code != http.StatusUnauthorized || repo.calls != 0 {
		t.Fatal(w.Code, w.Body.String(), repo.calls)
	}
	if w := requestUsage(h, "session", "/api/console/v1/workspaces/ws_b/usage"); w.Code != http.StatusForbidden || repo.calls != 0 {
		t.Fatal(w.Code, w.Body.String(), repo.calls)
	}
	w := requestUsage(h, "session", "/api/console/v1/workspaces/ws_a/usage")
	if w.Code != http.StatusOK || repo.calls != 1 {
		t.Fatal(w.Code, w.Body.String(), repo.calls)
	}
	body := w.Body.String()
	for _, want := range []string{`"limit_micro":"1000"`, `"consumed_micro":"125"`, `"reserved_micro":"75"`, `"available_micro":"800"`, `"charged_micro":"125"`, `"released_micro":"75"`} {
		if !strings.Contains(body, want) {
			t.Fatal("missing exact micro amount", want, body)
		}
	}
	for _, forbidden := range []string{"reservation_id", "price_version_id", "settlement_job", "provider_request_id", "credential", "secret"} {
		if strings.Contains(body, forbidden) {
			t.Fatal("usage response leaked internal field", forbidden, body)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("usage response is cacheable")
	}
}

func TestUsageObservabilityRejectsCorruptProjectionBeforeReturningIt(t *testing.T) {
	fixture := usageFixture()
	fixture.BudgetPeriods[0].ConsumedMicro = 999
	fixture.BudgetPeriods[0].ReservedMicro = 75
	repo := &usageRepository{snapshot: fixture}
	auth := &usageAuthorizer{actor: application.HumanUsageActor{UserID: "user_a"}}
	h := setupUsage(t, repo, auth)
	w := requestUsage(h, "session", "/api/console/v1/workspaces/ws_a/usage")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "USAGE_UNAVAILABLE") {
		t.Fatal(w.Code, w.Body.String())
	}

	auth.err = application.ErrObservabilityForbidden
	if w = requestUsage(h, "session", "/api/console/v1/workspaces/ws_a/usage"); w.Code != http.StatusForbidden {
		t.Fatal(w.Code, w.Body.String())
	}
	if !errors.Is(auth.err, application.ErrObservabilityForbidden) {
		t.Fatal(auth.err)
	}
}
