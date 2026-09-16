package domain

import (
	"errors"
	"time"
)

var ErrDangerousOperation = errors.New("invalid dangerous operation approval")

const (
	DangerousActionReleaseEmergencyDisable = "release.emergency_disable"
	DangerousActionSupportWorkspaceRead    = "support.workspace_read"

	DangerousSubjectWorkspaceMember = "workspace_member"
	DangerousSubjectPlatformStaff   = "platform_staff"

	DangerousTargetReleasePlan = "release_plan"
	DangerousTargetWorkspace   = "workspace"

	DangerousApprovalPending  = "pending"
	DangerousApprovalApproved = "approved"
	DangerousApprovalRejected = "rejected"
	DangerousApprovalConsumed = "consumed"
	DangerousApprovalExpired  = "expired"

	SupportScopeWorkspaceRead = "workspace:read"
	SupportScopeRunRead       = "run:read"
	SupportScopeUsageRead     = "usage:read"
)

const (
	MinDangerousApprovalTTL = time.Minute
	MaxDangerousApprovalTTL = 30 * time.Minute
	MinSupportAccessTTL     = 5 * time.Minute
	MaxSupportAccessTTL     = time.Hour
)

type DangerousOperationBinding struct {
	WorkspaceID, SubjectKind, SubjectID         string
	Action, TargetKind, TargetID, TargetVersion string
	ParametersSHA256, ParametersJSON, Currency  string
	AmountMicro                                 int64
	HasAmount                                   bool
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func (b DangerousOperationBinding) Valid() bool {
	if !validID(b.WorkspaceID) || !validID(b.SubjectID) || !validID(b.TargetID) || !validSHA256(b.ParametersSHA256) || len(b.ParametersJSON) < 2 || len(b.ParametersJSON) > 8192 {
		return false
	}
	if b.SubjectKind != DangerousSubjectWorkspaceMember && b.SubjectKind != DangerousSubjectPlatformStaff {
		return false
	}
	switch b.Action {
	case DangerousActionReleaseEmergencyDisable:
		if b.SubjectKind != DangerousSubjectWorkspaceMember || b.TargetKind != DangerousTargetReleasePlan || b.TargetVersion == "" || len(b.TargetVersion) > 128 {
			return false
		}
	case DangerousActionSupportWorkspaceRead:
		if b.SubjectKind != DangerousSubjectPlatformStaff || b.TargetKind != DangerousTargetWorkspace || b.TargetID != b.WorkspaceID || b.TargetVersion != "" {
			return false
		}
	default:
		return false
	}
	if b.HasAmount {
		return b.AmountMicro >= 0 && len(b.Currency) == 3
	}
	return b.AmountMicro == 0 && b.Currency == ""
}

type DangerousOperationApproval struct {
	WorkspaceID, ID, RequesterUserID, ReviewerUserID, DecisionNote, Reason string
	Binding                                                                DangerousOperationBinding
	State                                                                  string
	RequestedAt, ExpiresAt, ReviewedAt, ConsumedAt                         time.Time
}

func (a DangerousOperationApproval) Valid() bool {
	if !validID(a.WorkspaceID) || !validID(a.ID) || !validID(a.RequesterUserID) || a.WorkspaceID != a.Binding.WorkspaceID || !a.Binding.Valid() || len([]rune(a.Reason)) < 1 || len([]rune(a.Reason)) > 1000 {
		return false
	}
	if a.RequestedAt.IsZero() || a.ExpiresAt.Sub(a.RequestedAt) < MinDangerousApprovalTTL || a.ExpiresAt.Sub(a.RequestedAt) > MaxDangerousApprovalTTL {
		return false
	}
	switch a.State {
	case DangerousApprovalPending:
		return a.ReviewerUserID == "" && a.ReviewedAt.IsZero() && a.ConsumedAt.IsZero()
	case DangerousApprovalApproved, DangerousApprovalRejected:
		return validID(a.ReviewerUserID) && a.ReviewerUserID != a.RequesterUserID && !a.ReviewedAt.Before(a.RequestedAt) && a.ReviewedAt.Before(a.ExpiresAt) && a.ConsumedAt.IsZero()
	case DangerousApprovalConsumed:
		return validID(a.ReviewerUserID) && a.ReviewerUserID != a.RequesterUserID && !a.ReviewedAt.Before(a.RequestedAt) && !a.ConsumedAt.Before(a.ReviewedAt) && a.ConsumedAt.Before(a.ExpiresAt)
	case DangerousApprovalExpired:
		return a.ConsumedAt.IsZero()
	default:
		return false
	}
}

type JITSupportGrant struct {
	WorkspaceID, ID, ApprovalID, UserID, Reason string
	Scopes                                      []string
	CreatedAt, ExpiresAt, RevokedAt             time.Time
}

func validSupportScope(scope string) bool {
	return scope == SupportScopeWorkspaceRead || scope == SupportScopeRunRead || scope == SupportScopeUsageRead
}

func (g JITSupportGrant) Valid() bool {
	if !validID(g.WorkspaceID) || !validID(g.ID) || !validID(g.ApprovalID) || !validID(g.UserID) || len([]rune(g.Reason)) < 1 || len([]rune(g.Reason)) > 1000 || len(g.Scopes) < 1 || len(g.Scopes) > 3 {
		return false
	}
	seen := map[string]bool{}
	for _, scope := range g.Scopes {
		if !validSupportScope(scope) || seen[scope] {
			return false
		}
		seen[scope] = true
	}
	if g.CreatedAt.IsZero() || g.ExpiresAt.Sub(g.CreatedAt) < MinSupportAccessTTL || g.ExpiresAt.Sub(g.CreatedAt) > MaxSupportAccessTTL {
		return false
	}
	return g.RevokedAt.IsZero() || !g.RevokedAt.Before(g.CreatedAt)
}

func (g JITSupportGrant) Allows(scope string, at time.Time) bool {
	if !g.Valid() || !validSupportScope(scope) || at.Before(g.CreatedAt) || !at.Before(g.ExpiresAt) || !g.RevokedAt.IsZero() && !at.Before(g.RevokedAt) {
		return false
	}
	for _, granted := range g.Scopes {
		if granted == scope {
			return true
		}
	}
	return false
}
