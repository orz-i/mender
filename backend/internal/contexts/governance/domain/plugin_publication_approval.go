package domain

import (
	"strings"
	"time"
)

type PluginPublicationApproval struct {
	WorkspaceID, ID, PluginID, Version, RequesterUserID, ReviewerUserID, DecisionNote string
	TargetRevision                                                                    int64
	State                                                                             ApprovalState
	RequestedAt, ExpiresAt, ReviewedAt, ConsumedAt                                    time.Time
}

func validPluginID(value string) bool {
	if len(value) < 3 || len(value) > 128 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		ch := value[i]
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '-') {
			return false
		}
	}
	return true
}

func validPluginVersion(value string) bool {
	core, prerelease, hasPrerelease := strings.Cut(value, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return false
			}
		}
	}
	if !hasPrerelease {
		return true
	}
	if prerelease == "" {
		return false
	}
	for i := 0; i < len(prerelease); i++ {
		ch := prerelease[i]
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '-') {
			return false
		}
	}
	return true
}

func (a PluginPublicationApproval) Valid() bool {
	if !validID(a.WorkspaceID) || !validID(a.ID) || !validPluginID(a.PluginID) || !validPluginVersion(a.Version) || !validID(a.RequesterUserID) || a.TargetRevision < 1 || len([]rune(a.DecisionNote)) > 1000 || a.RequestedAt.IsZero() || !a.ExpiresAt.After(a.RequestedAt) {
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

func (a PluginPublicationApproval) EffectiveState(at time.Time) ApprovalState {
	if (a.State == ApprovalPending || a.State == ApprovalApproved) && !at.Before(a.ExpiresAt) {
		return ApprovalExpired
	}
	return a.State
}
