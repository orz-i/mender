package domain

import (
	"testing"
	"time"
)

func TestExecutionPolicyDecisionRequiresExactHashesAndKnownOutcome(t *testing.T) {
	at := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	d := ExecutionPolicyDecision{
		WorkspaceID: "ws_a", PolicyRevisionID: "exec_policy_1", PolicyRevision: 1,
		SubjectKind: "human", SubjectID: "user_a", ToolsetVersionID: "set_a", ToolVersionID: "tv_a", ConnectionID: "conn_a",
		ArgumentsHash: string(make([]byte, 64)), IdempotencyKeyHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RiskLevel: "critical", Outcome: ExecutionConfirmationRequired, ReasonCodes: []string{"tool_write_unsafe", "human_confirmation_required"}, Sequence: 1, EvaluatedAt: at,
	}
	if d.Valid() {
		t.Fatal("non-hex arguments hash accepted")
	}
	d.ArgumentsHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if !d.Valid() {
		t.Fatal("valid execution decision rejected")
	}
	d.SubjectKind = "agent"
	if d.Valid() {
		t.Fatal("unknown subject kind accepted")
	}
}

func TestExecutionConfirmationLifecycleIsExactAndSingleState(t *testing.T) {
	at := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	c := ExecutionConfirmation{
		WorkspaceID: "ws_a", ID: "confirm_a", UserID: "user_a", PolicyRevisionID: "exec_policy_1", PolicyRevision: 2,
		ToolsetVersionID: "set_a", ToolVersionID: "tv_a", ConnectionID: "conn_a",
		ArgumentsHash:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		IdempotencyKeyHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		RiskLevel:          "high", State: "active", CreatedAt: at, ExpiresAt: at.Add(5 * time.Minute),
	}
	if !c.Valid() {
		t.Fatal("valid active confirmation rejected")
	}
	c.State = "consumed"
	c.ConsumedAt = at.Add(time.Minute)
	if !c.Valid() {
		t.Fatal("valid consumed confirmation rejected")
	}
	c.ExpiredAt = at.Add(2 * time.Minute)
	if c.Valid() {
		t.Fatal("confirmation accepted both consumed and expired timestamps")
	}
}
