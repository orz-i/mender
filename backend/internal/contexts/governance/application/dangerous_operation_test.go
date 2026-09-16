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

func dangerousApproval(workspace, id, requester, releaseID string, at, expires time.Time) DangerousOperationApproval {
	return DangerousOperationApproval{WorkspaceID: workspace, ID: id, RequesterUserID: requester, SubjectKind: "workspace_member", SubjectID: requester,
		Action: "release.emergency_disable", TargetKind: "release_plan", TargetID: releaseID, TargetVersion: "2",
		ParametersJSON: `{"mode": "emergency_disable"}`, ParametersSHA256: "f2f39fe20f37679b6cdc443b04b23a4d1f9e943a7d5835e2724fdd0522c56adc",
		Reason: "incident", State: "pending", RequestedAt: at, ExpiresAt: expires}
}
func (r *dangerousRepoStub) ListDangerousOperations(context.Context, string, time.Time) ([]DangerousOperationApproval, error) {
	return nil, nil
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
