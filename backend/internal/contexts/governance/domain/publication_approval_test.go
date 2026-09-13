package domain

import (
	"testing"
	"time"
)

func approvalFixture() PublicationApproval {
	at := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	return PublicationApproval{WorkspaceID: "ws_a", ID: "approval_a", TargetKind: TargetToolVersion, TargetID: "tv_a", TargetRevision: 3, RequesterUserID: "maker_a", State: ApprovalPending, RequestedAt: at, ExpiresAt: at.Add(time.Hour)}
}

func TestPublicationApprovalMakerCheckerAndRevisionBinding(t *testing.T) {
	a := approvalFixture()
	if !a.Valid() || a.ReviewableBy("maker_a", a.RequestedAt.Add(time.Minute)) || !a.ReviewableBy("reviewer_a", a.RequestedAt.Add(time.Minute)) {
		t.Fatal("pending approval maker/checker invariant failed")
	}
	a.State = ApprovalApproved
	a.ReviewerUserID = "reviewer_a"
	a.ReviewedAt = a.RequestedAt.Add(time.Minute)
	if !a.Valid() || !a.Consumable(3, a.RequestedAt.Add(2*time.Minute)) || a.Consumable(4, a.RequestedAt.Add(2*time.Minute)) {
		t.Fatal("approved revision binding failed")
	}
	if a.EffectiveState(a.ExpiresAt) != ApprovalExpired || a.Consumable(3, a.ExpiresAt) {
		t.Fatal("expired approval remained effective")
	}
}

func TestPublicationApprovalRejectsSelfDecisionProjection(t *testing.T) {
	a := approvalFixture()
	a.State = ApprovalApproved
	a.ReviewerUserID = a.RequesterUserID
	a.ReviewedAt = a.RequestedAt.Add(time.Minute)
	if a.Valid() {
		t.Fatal("self-approved projection accepted")
	}
}
