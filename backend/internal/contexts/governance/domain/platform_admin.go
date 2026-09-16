package domain

import "time"

const (
	PlatformTargetWorkspace = "workspace"
	PlatformTargetProvider  = "provider"
	PlatformTargetIncident  = "incident"

	ProviderStateActive      = "active"
	ProviderStateQuarantined = "quarantined"

	IncidentSeverityInfo     = "info"
	IncidentSeverityWarning  = "warning"
	IncidentSeverityCritical = "critical"

	IncidentStateOpen     = "open"
	IncidentStateResolved = "resolved"
)

type WorkspaceAdminState struct {
	WorkspaceID         string
	Frozen              bool
	Revision            int64
	Reason, ActorUserID string
	UpdatedAt           time.Time
}

func validAdminReason(value string) bool {
	runes := []rune(value)
	return len(runes) >= 1 && len(runes) <= 1000
}

func (s WorkspaceAdminState) Valid() bool {
	return validID(s.WorkspaceID) && s.Revision > 0 && validAdminReason(s.Reason) && validID(s.ActorUserID) && !s.UpdatedAt.IsZero()
}

type ProviderAdminState struct {
	ProviderID          string
	State               string
	Revision            int64
	Reason, ActorUserID string
	UpdatedAt           time.Time
}

func (s ProviderAdminState) Valid() bool {
	if !validID(s.ProviderID) || s.Revision < 1 || !validAdminReason(s.Reason) || !validID(s.ActorUserID) || s.UpdatedAt.IsZero() {
		return false
	}
	return s.State == ProviderStateActive || s.State == ProviderStateQuarantined
}

type PlatformIncident struct {
	ID, TargetKind, TargetID        string
	Severity, Code, State           string
	OpenedByUserID, OpenReason      string
	ResolvedByUserID, Resolution    string
	Revision                        int64
	OpenedAt, UpdatedAt, ResolvedAt time.Time
}

func validIncidentCode(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '.' || ch == ':' || ch == '-') {
			return false
		}
	}
	return true
}

func (i PlatformIncident) Valid() bool {
	if !validID(i.ID) || !validID(i.TargetID) || !validIncidentCode(i.Code) || !validID(i.OpenedByUserID) || !validAdminReason(i.OpenReason) || i.Revision < 1 || i.OpenedAt.IsZero() || i.UpdatedAt.Before(i.OpenedAt) {
		return false
	}
	if i.TargetKind != PlatformTargetWorkspace && i.TargetKind != PlatformTargetProvider {
		return false
	}
	if i.Severity != IncidentSeverityInfo && i.Severity != IncidentSeverityWarning && i.Severity != IncidentSeverityCritical {
		return false
	}
	switch i.State {
	case IncidentStateOpen:
		return i.ResolvedByUserID == "" && i.Resolution == "" && i.ResolvedAt.IsZero()
	case IncidentStateResolved:
		return validID(i.ResolvedByUserID) && validAdminReason(i.Resolution) && !i.ResolvedAt.Before(i.OpenedAt) && !i.UpdatedAt.Before(i.ResolvedAt)
	default:
		return false
	}
}

type PlatformAdminAuditEvent struct {
	Sequence, TargetRevision                     int64
	EventKind, TargetKind, TargetID, ActorUserID string
	Reason                                       string
	OccurredAt                                   time.Time
}

func validPlatformAdminEventKind(value string) bool {
	switch value {
	case "workspace_frozen", "workspace_unfrozen", "provider_quarantined", "provider_restored", "incident_opened", "incident_resolved":
		return true
	default:
		return false
	}
}

func (e PlatformAdminAuditEvent) Valid() bool {
	if e.Sequence < 1 || e.TargetRevision < 1 || !validPlatformAdminEventKind(e.EventKind) || !validID(e.TargetID) || !validID(e.ActorUserID) || !validAdminReason(e.Reason) || e.OccurredAt.IsZero() {
		return false
	}
	return e.TargetKind == PlatformTargetWorkspace || e.TargetKind == PlatformTargetProvider || e.TargetKind == PlatformTargetIncident
}
