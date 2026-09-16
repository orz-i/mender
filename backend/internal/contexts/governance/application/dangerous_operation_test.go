package application

import (
	"context"
	"testing"
	"time"
)

type dangerousRepoStub struct {
	requester, releaseID string
	at, expires          time.Time
}

func TestDangerousCommerceRequestUsesPlatformAuthorityAndExactAmount(t *testing.T) {
	at := time.Date(2026, 9, 16, 3, 40, 0, 0, time.UTC)
	repo := &dangerousRepoStub{}
	auth := &dangerousAuthStub{}
	svc, err := NewDangerousOperationService(repo, auth, dangerousIDStub{}, dangerousClockStub{at})
	if err != nil {
		t.Fatal(err)
	}
	item, err := svc.RequestCommerceApproval(context.Background(), Actor{UserID: "finance_operator"}, "ws_billing", CommerceApprovalRequest{
		Action: "commerce.refund", BusinessKey: "refund:case_1", BasisKind: "usage_settlement", BasisID: "run_1", Direction: "credit", AmountMicro: 25, Currency: "USD", Reason: "customer refund", TTL: 10 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if auth.action != "platform:operate" || repo.requester != "finance_operator" || item.AmountMicro == nil || *item.AmountMicro != 25 || item.TargetVersion != "refund:case_1" {
		t.Fatal(auth.action, repo, item)
	}
}
func (r *dangerousRepoStub) RequestCommerceApproval(_ context.Context, workspace, id, requester, action, businessKey, basisKind, basisID, direction string, amount int64, currency, reason string, at, expires time.Time) (DangerousOperationApproval, error) {
	r.requester, r.at, r.expires = requester, at, expires
	targetKind := "billing_adjustment"
	if action == "commerce.refund" {
		targetKind = "billing_refund"
	}
	return DangerousOperationApproval{WorkspaceID: workspace, ID: id, RequesterUserID: requester, SubjectKind: "platform_staff", SubjectID: requester,
		Action: action, TargetKind: targetKind, TargetID: basisID, TargetVersion: businessKey,
		ParametersJSON:   `{"basis_id":"` + basisID + `","basis_kind":"` + basisKind + `","business_key":"` + businessKey + `","direction":"` + direction + `"}`,
		ParametersSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AmountMicro: &amount, Currency: currency,
		Reason: reason, State: "pending", RequestedAt: at, ExpiresAt: expires}, nil
}

func dangerousApproval(workspace, id, requester, releaseID string, at, expires time.Time) DangerousOperationApproval {
	return DangerousOperationApproval{WorkspaceID: workspace, ID: id, RequesterUserID: requester, SubjectKind: "workspace_member", SubjectID: requester,
		Action: "release.emergency_disable", TargetKind: "release_plan", TargetID: releaseID, TargetVersion: "2",
		ParametersJSON: `{"mode": "emergency_disable"}`, ParametersSHA256: "f2f39fe20f37679b6cdc443b04b23a4d1f9e943a7d5835e2724fdd0522c56adc",
		Reason: "incident", State: "pending", RequestedAt: at, ExpiresAt: expires}
}
func (r *dangerousRepoStub) ListDangerousOperations(context.Context, string, time.Time) ([]DangerousOperationApproval, error) {
	return nil, nil
}
func (r *dangerousRepoStub) GetDangerousOperation(_ context.Context, workspace, id string) (DangerousOperationApproval, error) {
	return dangerousApproval(workspace, id, "admin_release", "release_test", r.at, r.expires), nil
}
func (r *dangerousRepoStub) RequestReleaseEmergency(_ context.Context, workspace, id, requester, releaseID, _ string, at, expires time.Time) (DangerousOperationApproval, error) {
	r.requester, r.releaseID, r.at, r.expires = requester, releaseID, at, expires
	return dangerousApproval(workspace, id, requester, releaseID, at, expires), nil
}
func (r *dangerousRepoStub) ApproveDangerousOperation(context.Context, string, string, string, time.Time, string) (DangerousOperationApproval, error) {
	return DangerousOperationApproval{}, nil
}
func (r *dangerousRepoStub) RejectDangerousOperation(context.Context, string, string, string, time.Time, string) (DangerousOperationApproval, error) {
	return DangerousOperationApproval{}, nil
}

type dangerousIDStub struct{}

func (dangerousIDStub) NewDangerousOperationID() (string, error) { return "danger_test", nil }

type dangerousAuthStub struct{ action string }

func (a *dangerousAuthStub) Authenticate(context.Context, string) (Actor, error) {
	return Actor{UserID: "admin_release"}, nil
}
func (a *dangerousAuthStub) AuthenticateMutation(context.Context, string, string) (Actor, error) {
	return Actor{UserID: "admin_release"}, nil
}
func (a *dangerousAuthStub) Authorize(_ context.Context, _ Actor, _ string, action string) error {
	a.action = action
	return nil
}
func (a *dangerousAuthStub) AuthorizePlatform(_ context.Context, _ Actor, action string) error {
	a.action = action
	return nil
}

type dangerousClockStub struct{ at time.Time }

func (c dangerousClockStub) Now() time.Time { return c.at }

func TestDangerousReleaseRequestBindsServerClockAndRequester(t *testing.T) {
	at := time.Date(2026, 9, 16, 3, 30, 0, 0, time.UTC)
	repo := &dangerousRepoStub{}
	auth := &dangerousAuthStub{}
	svc, err := NewDangerousOperationService(repo, auth, dangerousIDStub{}, dangerousClockStub{at})
	if err != nil {
		t.Fatal(err)
	}
	item, err := svc.RequestReleaseEmergency(context.Background(), Actor{UserID: "admin_release"}, "ws_release", "release_test", "incident", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if auth.action != "release:manage" || repo.requester != "admin_release" || repo.releaseID != "release_test" || !repo.at.Equal(at) || !repo.expires.Equal(at.Add(10*time.Minute)) || item.TargetVersion != "2" {
		t.Fatal(auth.action, repo, item)
	}
}
