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

type policyRepo struct {
	createActor   string
	activateActor string
}

func (*policyRepo) ListPolicies(_ context.Context, ws string, _ int) (application.PolicySnapshot, error) {
	at := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	return application.PolicySnapshot{
		Revisions: []application.PolicyRevision{{WorkspaceID: ws, ID: "policy_2", Revision: 9007199254740993, State: "active", MaxRiskLevel: "high", DenyUnsafeWrite: true, CreatedByUserID: "owner_1", CreatedAt: at, ActivatedByUserID: "admin_1", ActivatedAt: at.Add(time.Second)}},
		Decisions: []application.PolicyDecision{{Sequence: 9007199254740995, WorkspaceID: ws, PolicyRevisionID: "policy_2", PolicyRevision: 9007199254740993, TargetKind: "toolset", TargetID: "set_1", TargetRevision: 9007199254740997, RiskLevel: "critical", Outcome: "deny", ReasonCodes: []string{"tool_write_unsafe", "risk_above_ceiling"}, EvaluatedAt: at.Add(2 * time.Second)}},
	}, nil
}
func (r *policyRepo) CreatePolicy(_ context.Context, ws, id, actor, risk string, denyUnsafe, denyMCP bool, at time.Time) (application.PolicyRevision, error) {
	r.createActor = actor
	return application.PolicyRevision{WorkspaceID: ws, ID: id, Revision: 3, State: "draft", MaxRiskLevel: risk, DenyUnsafeWrite: denyUnsafe, DenyMCPUnsafeWrite: denyMCP, CreatedByUserID: actor, CreatedAt: at}, nil
}
func (r *policyRepo) ActivatePolicy(_ context.Context, ws, id, actor string, at time.Time) (application.PolicyRevision, error) {
	r.activateActor = actor
	return application.PolicyRevision{WorkspaceID: ws, ID: id, Revision: 3, State: "active", MaxRiskLevel: "high", DenyUnsafeWrite: true, CreatedByUserID: "owner_1", CreatedAt: at.Add(-time.Minute), ActivatedByUserID: actor, ActivatedAt: at}, nil
}

func policyRouter(t *testing.T, repo *policyRepo, auth *reviewAuth) *gin.Engine {
	t.Helper()
	service, err := application.NewPolicy(repo, auth, reviewClock{at: time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewPublicationPolicy(service, auth)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	h.Register(r)
	return r
}

func TestPublicationPolicySnapshotKeepsExactIntegersAndReauthorizes(t *testing.T) {
	w := reviewRequest(policyRouter(t, &policyRepo{}, &reviewAuth{}), http.MethodGet, "/api/admin/v1/workspaces/ws_1/publication-policy", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"revision":"9007199254740993"`) || !strings.Contains(w.Body.String(), `"sequence":"9007199254740995"`) || !strings.Contains(w.Body.String(), `"target_revision":"9007199254740997"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = reviewRequest(policyRouter(t, &policyRepo{}, &reviewAuth{forbidden: true}), http.MethodGet, "/api/admin/v1/workspaces/ws_1/publication-policy", "", "")
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestPublicationPolicyMutationsUseSessionActorAndStrictJSON(t *testing.T) {
	repo := &policyRepo{}
	r := policyRouter(t, repo, &reviewAuth{})
	path := "/api/admin/v1/workspaces/ws_1/publication-policy/revisions"
	if w := reviewRequest(r, http.MethodPost, path, `{"id":"policy_3","max_risk_level":"high","deny_unsafe_write":true,"deny_mcp_unsafe_write":false}`, ""); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := reviewRequest(r, http.MethodPost, path, `{"id":"policy_3","max_risk_level":"high","deny_unsafe_write":true,"deny_mcp_unsafe_write":false,"script":"allow()"}`, "csrf_1"); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	w := reviewRequest(r, http.MethodPost, path, `{"id":"policy_3","max_risk_level":"high","deny_unsafe_write":true,"deny_mcp_unsafe_write":false}`, "csrf_1")
	if w.Code != 201 || repo.createActor != "reviewer_1" {
		t.Fatal(w.Code, repo.createActor, w.Body.String())
	}
	w = reviewRequest(r, http.MethodPost, "/api/admin/v1/workspaces/ws_1/publication-policy/revisions/policy_3/activate", "", "csrf_1")
	if w.Code != 200 || repo.activateActor != "reviewer_1" {
		t.Fatal(w.Code, repo.activateActor, w.Body.String())
	}
}
