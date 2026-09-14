package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type executionGovernanceRepo struct {
	filter                     application.ExecutionGovernanceFilter
	createActor, activateActor string
	maxUnconfirmed, maxMachine string
	denyUnsafe                 bool
	ttl                        int
}

func TestExecutionGovernancePolicyMutationsAreStrictAndUseSessionActor(t *testing.T) {
	repo := &executionGovernanceRepo{}
	r := executionGovernanceRouter(t, repo, &reviewAuth{})
	path := "/api/admin/v1/workspaces/ws_1/execution-governance/revisions"
	body := `{"id":"exec_policy_3","max_unconfirmed_risk_level":"high","max_machine_risk_level":"medium","deny_unsafe_write":true,"confirmation_ttl_seconds":120}`
	if w := reviewRequest(r, http.MethodPost, path, body, ""); w.Code != http.StatusForbidden {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := reviewRequest(r, http.MethodPost, path, body[:len(body)-1]+`,"script":"allow()"}`, "csrf_1"); w.Code != http.StatusBadRequest {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := reviewRequest(r, http.MethodPost, path, `{"id":"exec_policy_3","max_unconfirmed_risk_level":"medium","max_machine_risk_level":"critical","deny_unsafe_write":true,"confirmation_ttl_seconds":120}`, "csrf_1"); w.Code != http.StatusBadRequest {
		t.Fatal("machine ceiling wider than Human unconfirmed ceiling accepted", w.Code, w.Body.String())
	}
	w := reviewRequest(r, http.MethodPost, path, body, "csrf_1")
	if w.Code != http.StatusCreated || repo.createActor != "reviewer_1" || repo.maxUnconfirmed != "high" || repo.maxMachine != "medium" || !repo.denyUnsafe || repo.ttl != 120 {
		t.Fatal(w.Code, repo.createActor, repo.maxUnconfirmed, repo.maxMachine, repo.denyUnsafe, repo.ttl, w.Body.String())
	}
	w = reviewRequest(r, http.MethodPost, "/api/admin/v1/workspaces/ws_1/execution-governance/revisions/exec_policy_3/activate", "", "csrf_1")
	if w.Code != http.StatusOK || repo.activateActor != "reviewer_1" {
		t.Fatal(w.Code, repo.activateActor, w.Body.String())
	}
}

func (r *executionGovernanceRepo) ListExecutionGovernance(_ context.Context, ws string, filter application.ExecutionGovernanceFilter) (application.ExecutionGovernanceSnapshot, error) {
	r.filter = filter
	at := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	argsHash, idemHash := strings.Repeat("a", 64), strings.Repeat("b", 64)
	return application.ExecutionGovernanceSnapshot{
		Revisions: []application.ExecutionGovernancePolicyRevision{{
			WorkspaceID: ws, ID: "exec_policy_2", Revision: 9007199254740993, State: "active", MaxUnconfirmedRiskLevel: "low", MaxMachineRiskLevel: "low",
			DenyUnsafeWrite: true, ConfirmationTTLSeconds: 300, CreatedByUserID: "owner_1", ActivatedByUserID: "admin_1", CreatedAt: at.Add(-time.Minute), ActivatedAt: at,
		}},
		Decisions: []application.ExecutionRiskDecision{{
			WorkspaceID: ws, PolicyRevisionID: "exec_policy_2", PolicyRevision: 9007199254740993,
			SubjectKind: "human", SubjectID: "user_1", ToolsetVersionID: "toolset_v1", ToolVersionID: "tool_v1", ConnectionID: "conn_1",
			ArgumentsHash: argsHash, IdempotencyKeyHash: idemHash, RiskLevel: "critical", Outcome: "confirmation_required",
			ReasonCodes: []string{"tool_write_unsafe", "human_confirmation_required"}, Sequence: 9007199254740995, EvaluatedAt: at.Add(time.Second),
		}},
		Confirmations: []application.ExecutionGovernanceConfirmation{{
			WorkspaceID: ws, ID: "confirm_1", UserID: "user_1", PolicyRevisionID: "exec_policy_2", PolicyRevision: 9007199254740993,
			ToolsetVersionID: "toolset_v1", ToolVersionID: "tool_v1", ConnectionID: "conn_1", ArgumentsHash: argsHash, IdempotencyKeyHash: idemHash,
			RiskLevel: "critical", State: "active", EffectiveState: "expired", CreatedAt: at.Add(-30 * time.Second), ExpiresAt: at.Add(-time.Second),
		}},
		NextBeforeDecisionSequence:      9007199254740995,
		NextBeforeConfirmationCreatedAt: at.Add(-30 * time.Second),
		NextBeforeConfirmationID:        "confirm_1",
	}, nil
}

func (r *executionGovernanceRepo) CreateExecutionPolicy(_ context.Context, ws, id, actor, maxUnconfirmed, maxMachine string, denyUnsafe bool, ttl int, at time.Time) (application.ExecutionGovernancePolicyRevision, error) {
	r.createActor, r.maxUnconfirmed, r.maxMachine, r.denyUnsafe, r.ttl = actor, maxUnconfirmed, maxMachine, denyUnsafe, ttl
	return application.ExecutionGovernancePolicyRevision{
		WorkspaceID: ws, ID: id, Revision: 3, State: "draft", MaxUnconfirmedRiskLevel: maxUnconfirmed, MaxMachineRiskLevel: maxMachine,
		DenyUnsafeWrite: denyUnsafe, ConfirmationTTLSeconds: ttl, CreatedByUserID: actor, CreatedAt: at,
	}, nil
}

func (r *executionGovernanceRepo) ActivateExecutionPolicy(_ context.Context, ws, id, actor string, at time.Time) (application.ExecutionGovernancePolicyRevision, error) {
	r.activateActor = actor
	return application.ExecutionGovernancePolicyRevision{
		WorkspaceID: ws, ID: id, Revision: 3, State: "active", MaxUnconfirmedRiskLevel: "high", MaxMachineRiskLevel: "medium",
		DenyUnsafeWrite: true, ConfirmationTTLSeconds: 120, CreatedByUserID: "reviewer_1", CreatedAt: at.Add(-time.Minute), ActivatedByUserID: actor, ActivatedAt: at,
	}, nil
}

func executionGovernanceRouter(t *testing.T, repo *executionGovernanceRepo, auth *reviewAuth) *gin.Engine {
	t.Helper()
	service, err := application.NewExecutionGovernance(repo, auth, reviewClock{at: time.Date(2026, 9, 13, 13, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewExecutionGovernance(service, auth)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	handler.Register(r)
	return r
}

func TestExecutionGovernanceSnapshotUsesServerFiltersCursorsAndExactIntegers(t *testing.T) {
	repo := &executionGovernanceRepo{}
	r := executionGovernanceRouter(t, repo, &reviewAuth{})
	cursor := url.QueryEscape(time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano))
	path := "/api/admin/v1/workspaces/ws_1/execution-governance?tool_version_id=tool_v1&subject_kind=human&risk_level=critical&outcome=confirmation_required&policy_revision=9007199254740993&confirmation_state=expired&before_decision_sequence=9007199254740999&before_confirmation_created_at=" + cursor + "&before_confirmation_id=confirm_z&limit=25"
	w := reviewRequest(r, http.MethodGet, path, "", "")
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, wanted := range []string{`"revision":"9007199254740993"`, `"max_machine_risk_level":"low"`, `"confirmation_ttl_seconds":300`, `"sequence":"9007199254740995"`, `"subject_kind":"human"`, `"arguments_hash":"` + strings.Repeat("a", 64) + `"`, `"state":"expired"`, `"persisted_state":"active"`, `"next_before_decision_sequence":"9007199254740995"`} {
		if !strings.Contains(body, wanted) {
			t.Fatal("missing response fact", wanted, body)
		}
	}
	if repo.filter.Limit != 25 || repo.filter.PolicyRevision != 9007199254740993 || repo.filter.BeforeDecisionSequence != 9007199254740999 || repo.filter.BeforeConfirmationID != "confirm_z" || repo.filter.ConfirmationState != "expired" {
		t.Fatal("unexpected parsed filters", repo.filter)
	}
}

func TestExecutionGovernanceSnapshotRejectsInvalidCursorAndReauthorizes(t *testing.T) {
	r := executionGovernanceRouter(t, &executionGovernanceRepo{}, &reviewAuth{})
	w := reviewRequest(r, http.MethodGet, "/api/admin/v1/workspaces/ws_1/execution-governance?before_confirmation_created_at=2026-09-13T12:00:00Z", "", "")
	if w.Code != http.StatusBadRequest {
		t.Fatal(w.Code, w.Body.String())
	}
	w = reviewRequest(executionGovernanceRouter(t, &executionGovernanceRepo{}, &reviewAuth{forbidden: true}), http.MethodGet, "/api/admin/v1/workspaces/ws_1/execution-governance", "", "")
	if w.Code != http.StatusForbidden {
		t.Fatal(w.Code, w.Body.String())
	}
}
