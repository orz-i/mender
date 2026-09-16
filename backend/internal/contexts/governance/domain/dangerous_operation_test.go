package domain

import (
	"testing"
	"time"
)

func dangerousBinding() DangerousOperationBinding {
	return DangerousOperationBinding{
		WorkspaceID: "ws_a", SubjectKind: DangerousSubjectWorkspaceMember, SubjectID: "user_a",
		Action: DangerousActionReleaseEmergencyDisable, TargetKind: DangerousTargetReleasePlan, TargetID: "release_a", TargetVersion: "2",
		ParametersSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ParametersJSON: `{"mode":"emergency_disable"}`,
	}
}

func TestDangerousOperationBindingIsExactAndBounded(t *testing.T) {
	b := dangerousBinding()
	if !b.Valid() {
		t.Fatal("valid release binding rejected")
	}
	for name, mutate := range map[string]func(*DangerousOperationBinding){
		"subject":     func(v *DangerousOperationBinding) { v.SubjectID = "" },
		"version":     func(v *DangerousOperationBinding) { v.TargetVersion = "" },
		"hash":        func(v *DangerousOperationBinding) { v.ParametersSHA256 = "bad" },
		"amount_pair": func(v *DangerousOperationBinding) { v.HasAmount = true; v.AmountMicro = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			v := b
			mutate(&v)
			if v.Valid() {
				t.Fatal("invalid binding accepted", v)
			}
		})
	}
	support := DangerousOperationBinding{WorkspaceID: "ws_a", SubjectKind: DangerousSubjectPlatformStaff, SubjectID: "support_a", Action: DangerousActionSupportWorkspaceRead, TargetKind: DangerousTargetWorkspace, TargetID: "ws_a", ParametersSHA256: b.ParametersSHA256, ParametersJSON: `{"scopes":["run:read"]}`}
	if !support.Valid() {
		t.Fatal("valid support binding rejected")
	}
}

func TestDangerousApprovalMakerCheckerAndExpiry(t *testing.T) {
	at := time.Date(2026, 9, 16, 3, 0, 0, 0, time.UTC)
	a := DangerousOperationApproval{WorkspaceID: "ws_a", ID: "approval_a", RequesterUserID: "user_a", Binding: dangerousBinding(), Reason: "incident response", State: DangerousApprovalPending, RequestedAt: at, ExpiresAt: at.Add(10 * time.Minute)}
	if !a.Valid() {
		t.Fatal("pending approval rejected")
	}
	a.State, a.ReviewerUserID, a.ReviewedAt = DangerousApprovalApproved, "user_b", at.Add(time.Minute)
	if !a.Valid() {
		t.Fatal("approved maker/checker rejected")
	}
	a.ReviewerUserID = a.RequesterUserID
	if a.Valid() {
		t.Fatal("self approval accepted")
	}
}

func TestJITSupportGrantIsReadOnlyBoundedAndTimeLimited(t *testing.T) {
	at := time.Date(2026, 9, 16, 3, 0, 0, 0, time.UTC)
	g := JITSupportGrant{WorkspaceID: "ws_a", ID: "jit_a", ApprovalID: "approval_a", UserID: "support_a", Reason: "customer incident", Scopes: []string{SupportScopeWorkspaceRead, SupportScopeRunRead}, CreatedAt: at, ExpiresAt: at.Add(20 * time.Minute)}
	if !g.Valid() || !g.Allows(SupportScopeRunRead, at.Add(time.Minute)) || g.Allows("run:cancel", at.Add(time.Minute)) || g.Allows(SupportScopeRunRead, g.ExpiresAt) {
		t.Fatal("JIT support grant boundary failed", g)
	}
	dup := g
	dup.Scopes = []string{SupportScopeRunRead, SupportScopeRunRead}
	if dup.Valid() {
		t.Fatal("duplicate JIT scope accepted")
	}
}
