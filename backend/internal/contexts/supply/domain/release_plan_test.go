package domain

import (
	"errors"
	"testing"
	"time"
)

func releasePlanFixture(t *testing.T) (ReleasePlan, time.Time) {
	t.Helper()
	at := time.Date(2026, 9, 15, 22, 0, 0, 0, time.UTC)
	plan, err := NewReleasePlan(ReleasePlanSnapshot{
		WorkspaceID:                 "ws_release",
		ID:                          "release_example",
		PluginID:                    "example.search",
		PluginVersion:               "2.0.0",
		ToolsetVersionID:            "toolset_release",
		ToolVersionID:               "toolv_search",
		ProviderID:                  "provider_search",
		StableDeploymentRevision:    "deploy_stable",
		CandidateDeploymentRevision: "deploy_candidate",
		CreatedAt:                   at,
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan, at
}

func TestReleasePlanLifecycleRoutesOnlyNewTrafficDecision(t *testing.T) {
	plan, at := releasePlanFixture(t)
	if got, blocked := plan.Route(); got != "deploy_stable" || blocked {
		t.Fatal(got, blocked)
	}
	if err := plan.StartCanary(at.Add(time.Minute), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, blocked := plan.Route(); got != "deploy_candidate" || blocked {
		t.Fatal(got, blocked)
	}
	if err := plan.Promote(at.Add(5 * time.Minute)); !errors.Is(err, ErrReleaseTransition) {
		t.Fatal("promoted before observation window", err)
	}
	if err := plan.Promote(at.Add(11 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, blocked := plan.Route(); got != "deploy_candidate" || blocked {
		t.Fatal(got, blocked)
	}
	if err := plan.Drain(at.Add(12 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, blocked := plan.Route(); got != "deploy_stable" || blocked {
		t.Fatal("drain must stop candidate from receiving new traffic", got, blocked)
	}
	if err := plan.Rollback(at.Add(13 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, blocked := plan.Route(); got != "deploy_stable" || blocked {
		t.Fatal(got, blocked)
	}
	if plan.Snapshot().Revision != 5 {
		t.Fatal("unexpected release revision", plan.Snapshot().Revision)
	}
}

func TestReleasePlanEmergencyDisableIsNotRollback(t *testing.T) {
	plan, at := releasePlanFixture(t)
	if err := plan.StartCanary(at.Add(time.Minute), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := plan.EmergencyDisable(at.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, blocked := plan.Route(); got != "" || !blocked {
		t.Fatal("emergency disable must block new release routing", got, blocked)
	}
	if plan.Snapshot().State != ReleaseDisabled || plan.Snapshot().RolledBackAt.IsZero() == false {
		t.Fatal("disable was conflated with rollback", plan.Snapshot())
	}
	if err := plan.Rollback(at.Add(3 * time.Minute)); err != nil {
		t.Fatal("stable recovery after emergency disable failed", err)
	}
	if got, blocked := plan.Route(); got != "deploy_stable" || blocked || plan.Snapshot().DisabledAt.IsZero() {
		t.Fatal("rollback must recover stable routing without erasing disable history", got, blocked, plan.Snapshot())
	}
}

func TestReleasePlanRejectsUnsafeFactsAndTransitions(t *testing.T) {
	plan, at := releasePlanFixture(t)
	bad := plan.Snapshot()
	bad.CandidateDeploymentRevision = bad.StableDeploymentRevision
	if _, err := RestoreReleasePlan(bad); !errors.Is(err, ErrInvalidReleasePlan) {
		t.Fatal("same stable/candidate deployment accepted", err)
	}
	if err := plan.StartCanary(at.Add(time.Minute), 30*time.Second); !errors.Is(err, ErrReleaseTransition) {
		t.Fatal("short canary window accepted", err)
	}
	if err := plan.Rollback(at.Add(time.Minute)); !errors.Is(err, ErrReleaseTransition) {
		t.Fatal("draft rollback accepted", err)
	}
}
