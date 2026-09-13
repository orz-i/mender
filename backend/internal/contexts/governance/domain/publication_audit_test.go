package domain

import (
	"testing"
	"time"
)

func TestPublicationAuditEventValidation(t *testing.T) {
	at := time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)
	base := PublicationAuditEvent{
		Sequence: 1, WorkspaceID: "ws_audit", ApprovalID: "approval_1", TargetKind: TargetToolVersion,
		TargetID: "tv_audit", TargetRevision: 2, ObservedRevision: 2, EventKind: AuditApprovalSubmitted,
		ActorUserID: "maker_audit", OccurredAt: at,
	}
	if !base.Valid() {
		t.Fatal("valid publication audit event rejected")
	}
	for name, mutate := range map[string]func(*PublicationAuditEvent){
		"zero sequence":     func(v *PublicationAuditEvent) { v.Sequence = 0 },
		"invalid target":    func(v *PublicationAuditEvent) { v.TargetKind = "other" },
		"missing approval":  func(v *PublicationAuditEvent) { v.ApprovalID = "" },
		"missing actor":     func(v *PublicationAuditEvent) { v.ActorUserID = "" },
		"negative observed": func(v *PublicationAuditEvent) { v.ObservedRevision = -1 },
		"unknown event":     func(v *PublicationAuditEvent) { v.EventKind = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			value := base
			mutate(&value)
			if value.Valid() {
				t.Fatal("invalid publication audit event accepted")
			}
		})
	}
	retired := base
	retired.ApprovalID = ""
	retired.ActorUserID = ""
	retired.EventKind = AuditPublicationRetired
	if !retired.Valid() {
		t.Fatal("publication retirement audit event rejected")
	}
}
