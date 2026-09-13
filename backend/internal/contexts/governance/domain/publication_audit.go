package domain

import "time"

type PublicationAuditKind string

const (
	AuditBaseline             PublicationAuditKind = "audit_baseline"
	AuditApprovalSubmitted    PublicationAuditKind = "approval_submitted"
	AuditApprovalApproved     PublicationAuditKind = "approval_approved"
	AuditApprovalRejected     PublicationAuditKind = "approval_rejected"
	AuditApprovalExpired      PublicationAuditKind = "approval_expired"
	AuditApprovalConsumed     PublicationAuditKind = "approval_consumed"
	AuditPublicationCommitted PublicationAuditKind = "publication_committed"
	AuditPublicationRetired   PublicationAuditKind = "publication_retired"
)

type PublicationAuditEvent struct {
	Sequence                          int64
	WorkspaceID, ApprovalID, TargetID string
	TargetKind                        PublicationTarget
	TargetRevision, ObservedRevision  int64
	EventKind                         PublicationAuditKind
	ActorUserID, ReasonCode, Note     string
	OccurredAt                        time.Time
}

func (e PublicationAuditEvent) Valid() bool {
	if e.Sequence < 1 || !validID(e.WorkspaceID) || !validID(e.TargetID) || e.TargetRevision < 1 || e.ObservedRevision < 0 || e.OccurredAt.IsZero() || len(e.Note) > 1000 || len(e.ReasonCode) > 64 {
		return false
	}
	if e.TargetKind != TargetToolVersion && e.TargetKind != TargetToolset {
		return false
	}
	if e.ApprovalID != "" && !validID(e.ApprovalID) {
		return false
	}
	if e.ActorUserID != "" && !validID(e.ActorUserID) {
		return false
	}
	switch e.EventKind {
	case AuditBaseline:
		return e.ApprovalID != ""
	case AuditApprovalSubmitted, AuditApprovalApproved, AuditApprovalRejected:
		return e.ApprovalID != "" && e.ActorUserID != ""
	case AuditApprovalExpired, AuditApprovalConsumed, AuditPublicationCommitted:
		return e.ApprovalID != ""
	case AuditPublicationRetired:
		return e.ApprovalID == ""
	default:
		return false
	}
}
