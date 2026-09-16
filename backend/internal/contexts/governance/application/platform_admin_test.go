package application

import (
	"context"
	"testing"
	"time"
)

type platformAdminAuthStub struct {
	action string
	err    error
}

func (a *platformAdminAuthStub) Authenticate(context.Context, string) (Actor, error) {
	return Actor{UserID: "operator_a"}, nil
}
func (a *platformAdminAuthStub) AuthenticateMutation(context.Context, string, string) (Actor, error) {
	return Actor{UserID: "operator_a"}, nil
}
func (a *platformAdminAuthStub) AuthorizePlatform(_ context.Context, _ Actor, action string) error {
	a.action = action
	return a.err
}

type platformAdminIDStub struct{}

func (platformAdminIDStub) NewPlatformIncidentID() (string, error) { return "incident_test", nil }

type platformAdminClockStub struct{ at time.Time }

func (c platformAdminClockStub) Now() time.Time { return c.at }

type platformAdminRepoStub struct {
	workspaceExpected int64
	workspaceActor    string
	providerExpected  int64
}

func (r *platformAdminRepoStub) ListPlatformWorkspaces(context.Context, string) ([]PlatformWorkspace, error) {
	return nil, nil
}
func (r *platformAdminRepoStub) SetPlatformWorkspaceFrozen(_ context.Context, workspace string, expected int64, frozen bool, actor, reason string, at time.Time) (PlatformWorkspace, error) {
	r.workspaceExpected, r.workspaceActor = expected, actor
	return PlatformWorkspace{WorkspaceID: workspace, Frozen: frozen, Revision: expected + 1, Reason: reason, ActorUserID: actor, UpdatedAt: at, CreatedAt: at.Add(-time.Hour)}, nil
}
func (r *platformAdminRepoStub) ListPlatformProviders(context.Context, string) ([]PlatformProvider, error) {
	return nil, nil
}
func (r *platformAdminRepoStub) SetPlatformProviderState(_ context.Context, provider string, expected int64, state, actor, reason string, at time.Time) (PlatformProvider, error) {
	r.providerExpected = expected
	return PlatformProvider{ProviderID: provider, State: state, Revision: expected + 1, Reason: reason, ActorUserID: actor, UpdatedAt: at, DeploymentCount: 2, ActiveDeploymentCount: 1}, nil
}
func (r *platformAdminRepoStub) OpenPlatformIncident(_ context.Context, id, targetKind, targetID, severity, code, actor, reason string, at time.Time) (PlatformIncident, error) {
	return PlatformIncident{ID: id, TargetKind: targetKind, TargetID: targetID, Severity: severity, Code: code, State: "open", OpenedByUserID: actor, OpenReason: reason, Revision: 1, OpenedAt: at, UpdatedAt: at}, nil
}
func (r *platformAdminRepoStub) ResolvePlatformIncident(_ context.Context, id string, expected int64, actor, resolution string, at time.Time) (PlatformIncident, error) {
	return PlatformIncident{ID: id, TargetKind: "provider", TargetID: "provider_a", Severity: "critical", Code: "provider.anomaly", State: "resolved", OpenedByUserID: "operator_a", OpenReason: "incident", ResolvedByUserID: actor, Resolution: resolution, Revision: expected + 1, OpenedAt: at.Add(-time.Hour), UpdatedAt: at, ResolvedAt: at}, nil
}
func (r *platformAdminRepoStub) ListPlatformIncidents(context.Context, string, PlatformIncidentFilter) ([]PlatformIncident, error) {
	return nil, nil
}
func (r *platformAdminRepoStub) ListPlatformAudit(context.Context, string, PlatformAuditFilter) ([]PlatformAdminAuditEvent, error) {
	return nil, nil
}

func TestPlatformAdminWorkspaceMutationUsesPlatformAuthorityAndServerClock(t *testing.T) {
	at := time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC)
	repo, auth := &platformAdminRepoStub{}, &platformAdminAuthStub{}
	svc, err := NewPlatformAdminService(repo, auth, platformAdminIDStub{}, platformAdminClockStub{at})
	if err != nil {
		t.Fatal(err)
	}
	item, err := svc.SetWorkspaceFrozen(context.Background(), Actor{UserID: "operator_a"}, "ws_a", 7, true, "security incident")
	if err != nil || auth.action != "platform:operate" || repo.workspaceExpected != 7 || repo.workspaceActor != "operator_a" || item.Revision != 8 || !item.UpdatedAt.Equal(at) {
		t.Fatal(err, auth.action, repo, item)
	}
}

func TestPlatformAdminIncidentIDAndActorAreServerOwned(t *testing.T) {
	at := time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC)
	svc, _ := NewPlatformAdminService(&platformAdminRepoStub{}, &platformAdminAuthStub{}, platformAdminIDStub{}, platformAdminClockStub{at})
	item, err := svc.OpenIncident(context.Background(), Actor{UserID: "operator_a"}, "provider", "provider_a", "critical", "provider.anomaly", "callback divergence")
	if err != nil || item.ID != "incident_test" || item.OpenedByUserID != "operator_a" || item.Revision != 1 {
		t.Fatal(err, item)
	}
}
