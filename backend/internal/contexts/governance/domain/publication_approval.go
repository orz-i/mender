package domain

import "time"

type PublicationTarget string

const (
	TargetToolVersion PublicationTarget = "tool_version"
	TargetToolset     PublicationTarget = "toolset"
)

type ApprovalState string

const (
	ApprovalPending  ApprovalState = "pending"
	ApprovalApproved ApprovalState = "approved"
	ApprovalRejected ApprovalState = "rejected"
	ApprovalConsumed ApprovalState = "consumed"
	ApprovalExpired  ApprovalState = "expired"
)

type PublicationApproval struct {
	WorkspaceID, ID, TargetID, RequesterUserID, ReviewerUserID, DecisionNote string
	TargetKind                                                               PublicationTarget
	TargetRevision                                                           int64
	State                                                                    ApprovalState
	RequestedAt, ExpiresAt, ReviewedAt, ConsumedAt                           time.Time
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (a PublicationApproval) Valid() bool {
	if !validID(a.WorkspaceID) || !validID(a.ID) || !validID(a.TargetID) || !validID(a.RequesterUserID) || a.TargetRevision < 1 || len(a.DecisionNote) > 1000 || a.RequestedAt.IsZero() || !a.ExpiresAt.After(a.RequestedAt) {
		return false
	}
	if a.TargetKind != TargetToolVersion && a.TargetKind != TargetToolset {
		return false
	}
	switch a.State {
	case ApprovalPending:
		return a.ReviewerUserID == "" && a.ReviewedAt.IsZero() && a.ConsumedAt.IsZero()
	case ApprovalApproved, ApprovalRejected:
		return validID(a.ReviewerUserID) && a.ReviewerUserID != a.RequesterUserID && !a.ReviewedAt.Before(a.RequestedAt) && a.ConsumedAt.IsZero()
	case ApprovalConsumed:
		return validID(a.ReviewerUserID) && a.ReviewerUserID != a.RequesterUserID && !a.ReviewedAt.Before(a.RequestedAt) && !a.ConsumedAt.Before(a.ReviewedAt)
	case ApprovalExpired:
		return a.ConsumedAt.IsZero()
	default:
		return false
	}
}

func (a PublicationApproval) EffectiveState(at time.Time) ApprovalState {
	if (a.State == ApprovalPending || a.State == ApprovalApproved) && !at.Before(a.ExpiresAt) {
		return ApprovalExpired
	}
	return a.State
}

func (a PublicationApproval) ReviewableBy(reviewer string, at time.Time) bool {
	return a.Valid() && a.State == ApprovalPending && validID(reviewer) && reviewer != a.RequesterUserID && at.Before(a.ExpiresAt)
}

func (a PublicationApproval) Consumable(revision int64, at time.Time) bool {
	return a.Valid() && a.State == ApprovalApproved && a.TargetRevision == revision && at.Before(a.ExpiresAt)
}
