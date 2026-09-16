package application

import (
	"context"
	"errors"
	"testing"
	"time"
)

type releaseAuthStub struct {
	action string
	err    error
}

func (a *releaseAuthStub) Authenticate(context.Context, string) (PublisherActor, error) {
	return PublisherActor{UserID: "admin_release"}, nil
}
func (a *releaseAuthStub) AuthenticateMutation(context.Context, string, string) (PublisherActor, error) {
	return PublisherActor{UserID: "admin_release"}, nil
}
func (a *releaseAuthStub) Authorize(_ context.Context, _ PublisherActor, _ string, action string) error {
	a.action = action
	return a.err
}

type releaseIDStub struct{}

func (releaseIDStub) NewID() (string, error) { return "release_test", nil }

type releaseClockStub struct{ at time.Time }

func (c releaseClockStub) Now() time.Time { return c.at }

type releaseRepoStub struct {
	canaryUntil time.Time
	createCalls int
}

func releaseProjection(state string, revision int64, at time.Time) ReleasePlan {
	v := ReleasePlan{
		WorkspaceID: "ws_release", ID: "release_test", PluginID: "example.search", PluginVersion: "2.0.0",
		ToolsetVersionID: "set_release", ToolVersionID: "tv_release", ProviderID: "provider_release",
		StableDeploymentRevision: "deploy_stable", CandidateDeploymentRevision: "deploy_candidate",
		Revision: revision, State: state, CreatedByUserID: "admin_release", CreatedAt: at, UpdatedAt: at,
	}
	if state == "canary" {
		v.CanaryStartedAt = at
		v.ObservationUntil = at.Add(10 * time.Minute)
	}
	return v
}
func (r *releaseRepoStub) ReleaseSnapshot(context.Context, string) (ReleaseSnapshot, error) {
	return ReleaseSnapshot{}, nil
}
func (r *releaseRepoStub) CreateReleasePlan(_ context.Context, _, _, _ string, _ ReleasePlanInput, at time.Time) (ReleasePlan, error) {
	r.createCalls++
	return releaseProjection("draft", 1, at), nil
}
func (r *releaseRepoStub) StartReleaseCanary(_ context.Context, _, _, _, _ string, at, until time.Time) (ReleasePlan, error) {
	r.canaryUntil = until
	v := releaseProjection("canary", 2, at)
	v.ObservationUntil = until
	return v, nil
}
func (r *releaseRepoStub) PromoteRelease(context.Context, string, string, string, string, time.Time) (ReleasePlan, error) {
	return ReleasePlan{}, nil
}
func (r *releaseRepoStub) DrainRelease(context.Context, string, string, string, string, time.Time) (ReleasePlan, error) {
	return ReleasePlan{}, nil
}
func (r *releaseRepoStub) RollbackRelease(context.Context, string, string, string, string, time.Time) (ReleasePlan, error) {
	return ReleasePlan{}, nil
}
func (r *releaseRepoStub) EmergencyDisableRelease(context.Context, string, string, string, string, time.Time) (ReleasePlan, error) {
	return ReleasePlan{}, nil
}

func TestReleaseGovernanceUsesAdminAuthorityAndServerClock(t *testing.T) {
	at := time.Date(2026, 9, 16, 2, 0, 0, 0, time.UTC)
	repo := &releaseRepoStub{}
	auth := &releaseAuthStub{}
	service, err := NewReleaseGovernance(repo, auth, releaseIDStub{}, releaseClockStub{at: at})
	if err != nil {
		t.Fatal(err)
	}
	input := ReleasePlanInput{PluginID: "example.search", PluginVersion: "2.0.0", ToolsetVersionID: "set_release", ToolVersionID: "tv_release", ProviderID: "provider_release", StableDeploymentRevision: "deploy_stable", CandidateDeploymentRevision: "deploy_candidate", Reason: "prepare canary"}
	if _, err = service.Create(context.Background(), PublisherActor{UserID: "admin_release"}, "ws_release", input); err != nil {
		t.Fatal(err)
	}
	if auth.action != "release:manage" || repo.createCalls != 1 {
		t.Fatal(auth.action, repo.createCalls)
	}
	if _, err = service.StartCanary(context.Background(), PublisherActor{UserID: "admin_release"}, "ws_release", "release_test", "start canary", 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if !repo.canaryUntil.Equal(at.Add(10 * time.Minute)) {
		t.Fatal("observation window not derived from server clock", repo.canaryUntil)
	}
}

func TestReleaseGovernanceFailsBeforeRepositoryWhenUnauthorized(t *testing.T) {
	repo := &releaseRepoStub{}
	auth := &releaseAuthStub{err: ErrPublicationForbidden}
	service, _ := NewReleaseGovernance(repo, auth, releaseIDStub{}, releaseClockStub{at: time.Date(2026, 9, 16, 2, 0, 0, 0, time.UTC)})
	input := ReleasePlanInput{PluginID: "example.search", PluginVersion: "2.0.0", ToolsetVersionID: "set_release", ToolVersionID: "tv_release", ProviderID: "provider_release", StableDeploymentRevision: "deploy_stable", CandidateDeploymentRevision: "deploy_candidate", Reason: "prepare canary"}
	_, err := service.Create(context.Background(), PublisherActor{UserID: "admin_release"}, "ws_release", input)
	if !errors.Is(err, ErrPublicationForbidden) || repo.createCalls != 0 {
		t.Fatal(err, repo.createCalls)
	}
}
