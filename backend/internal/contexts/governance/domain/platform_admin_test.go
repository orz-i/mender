package domain

import (
	"testing"
	"time"
)

func TestWorkspaceAndProviderAdminStatesAreRevisionedAndReasoned(t *testing.T) {
	at := time.Date(2026, 9, 16, 4, 0, 0, 0, time.UTC)
	workspace := WorkspaceAdminState{WorkspaceID: "ws_a", Frozen: true, Revision: 2, Reason: "security incident", ActorUserID: "operator_a", UpdatedAt: at}
	provider := ProviderAdminState{ProviderID: "provider_a", State: ProviderStateQuarantined, Revision: 3, Reason: "upstream anomaly", ActorUserID: "operator_a", UpdatedAt: at}
	if !workspace.Valid() || !provider.Valid() {
		t.Fatal("valid admin state rejected", workspace, provider)
	}
	workspace.Revision = 0
	provider.State = "deleted"
	if workspace.Valid() || provider.Valid() {
		t.Fatal("invalid admin state accepted", workspace, provider)
	}
}

func TestPlatformIncidentContainsOnlyBoundedOperationalFacts(t *testing.T) {
	at := time.Date(2026, 9, 16, 4, 0, 0, 0, time.UTC)
	incident := PlatformIncident{
		ID: "incident_a", TargetKind: PlatformTargetProvider, TargetID: "provider_a", Severity: IncidentSeverityCritical,
		Code: "provider.callback_anomaly", State: IncidentStateOpen, OpenedByUserID: "operator_a", OpenReason: "callback divergence", Revision: 1,
		OpenedAt: at, UpdatedAt: at,
	}
	if !incident.Valid() {
		t.Fatal("valid open incident rejected", incident)
	}
	incident.State, incident.ResolvedByUserID, incident.Resolution = IncidentStateResolved, "operator_b", "provider stabilized"
	incident.ResolvedAt, incident.UpdatedAt, incident.Revision = at.Add(time.Minute), at.Add(time.Minute), 2
	if !incident.Valid() {
		t.Fatal("valid resolved incident rejected", incident)
	}
	incident.Resolution = ""
	if incident.Valid() {
		t.Fatal("resolved incident without resolution accepted")
	}
}

func TestPlatformAdminAuditEventIsAppendOnlyProjectionShape(t *testing.T) {
	event := PlatformAdminAuditEvent{
		Sequence: 7, TargetRevision: 2, EventKind: "workspace_frozen", TargetKind: PlatformTargetWorkspace,
		TargetID: "ws_a", ActorUserID: "operator_a", Reason: "security incident", OccurredAt: time.Date(2026, 9, 16, 4, 0, 0, 0, time.UTC),
	}
	if !event.Valid() {
		t.Fatal("valid audit event rejected", event)
	}
	event.EventKind = "workspace_deleted"
	if event.Valid() {
		t.Fatal("unsupported audit event accepted")
	}
}
