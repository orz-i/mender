package domain

import (
	"testing"
	"time"
)

func TestPublicationPolicyRevisionLifecycleValidation(t *testing.T) {
	at := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	draft := PublicationPolicyRevision{WorkspaceID: "ws_1", ID: "policy_2", Revision: 2, State: PolicyDraft, MaxRiskLevel: RiskHigh, CreatedByUserID: "admin_1", CreatedAt: at}
	if !draft.Valid() {
		t.Fatal("valid draft policy rejected")
	}
	active := draft
	active.State = PolicyActive
	active.ActivatedByUserID = "admin_2"
	active.ActivatedAt = at.Add(time.Second)
	if !active.Valid() {
		t.Fatal("valid active policy rejected")
	}
	active.RetiredAt = at.Add(2 * time.Second)
	if active.Valid() {
		t.Fatal("active policy accepted retired timestamp")
	}
}

func TestPublicationPolicyDecisionValidation(t *testing.T) {
	d := PublicationPolicyDecision{
		Sequence: 9007199254740993, WorkspaceID: "ws_1", PolicyRevisionID: "policy_2", PolicyRevision: 2,
		TargetKind: TargetToolset, TargetID: "set_1", TargetRevision: 7, RiskLevel: RiskCritical, Outcome: PolicyDeny,
		ReasonCodes: []string{"tool_write_unsafe", "mcp_unsafe_write_denied"}, EvaluatedAt: time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC),
	}
	if !d.Valid() {
		t.Fatal("valid policy decision rejected")
	}
	d.ReasonCodes = []string{"arbitrary_script_result"}
	if d.Valid() {
		t.Fatal("unknown policy reason accepted")
	}
}
