package domain

import "time"

type PublicationRiskLevel string

const (
	RiskLow      PublicationRiskLevel = "low"
	RiskMedium   PublicationRiskLevel = "medium"
	RiskHigh     PublicationRiskLevel = "high"
	RiskCritical PublicationRiskLevel = "critical"
)

func (r PublicationRiskLevel) Valid() bool {
	return r == RiskLow || r == RiskMedium || r == RiskHigh || r == RiskCritical
}

type PolicyRevisionState string

const (
	PolicyDraft   PolicyRevisionState = "draft"
	PolicyActive  PolicyRevisionState = "active"
	PolicyRetired PolicyRevisionState = "retired"
)

type PublicationPolicyRevision struct {
	WorkspaceID, ID                     string
	Revision                            int64
	State                               PolicyRevisionState
	MaxRiskLevel                        PublicationRiskLevel
	DenyUnsafeWrite, DenyMCPUnsafeWrite bool
	CreatedByUserID, ActivatedByUserID  string
	CreatedAt, ActivatedAt, RetiredAt   time.Time
}

func (p PublicationPolicyRevision) Valid() bool {
	if !validID(p.WorkspaceID) || !validID(p.ID) || p.Revision < 1 || !p.MaxRiskLevel.Valid() || p.CreatedAt.IsZero() {
		return false
	}
	if p.CreatedByUserID != "" && !validID(p.CreatedByUserID) || p.ActivatedByUserID != "" && !validID(p.ActivatedByUserID) {
		return false
	}
	switch p.State {
	case PolicyDraft:
		return p.ActivatedByUserID == "" && p.ActivatedAt.IsZero() && p.RetiredAt.IsZero()
	case PolicyActive:
		return !p.ActivatedAt.IsZero() && !p.ActivatedAt.Before(p.CreatedAt) && p.RetiredAt.IsZero()
	case PolicyRetired:
		return !p.ActivatedAt.IsZero() && !p.ActivatedAt.Before(p.CreatedAt) && !p.RetiredAt.Before(p.ActivatedAt)
	default:
		return false
	}
}

type PolicyDecisionOutcome string

const (
	PolicyAllow PolicyDecisionOutcome = "allow"
	PolicyDeny  PolicyDecisionOutcome = "deny"
)

var policyReasonCodes = map[string]bool{
	"tool_read_only_safe": true, "tool_read_only_non_safe": true, "tool_write_idempotent": true,
	"tool_write_unsafe": true, "tool_write_contract_mismatch": true, "toolset_empty": true,
	"within_risk_ceiling": true, "risk_above_ceiling": true, "unsafe_write_denied": true, "mcp_unsafe_write_denied": true,
}

type PublicationPolicyDecision struct {
	Sequence                      int64
	WorkspaceID, PolicyRevisionID string
	PolicyRevision                int64
	TargetKind                    PublicationTarget
	TargetID                      string
	TargetRevision                int64
	RiskLevel                     PublicationRiskLevel
	Outcome                       PolicyDecisionOutcome
	ReasonCodes                   []string
	EvaluatedAt                   time.Time
}

func (d PublicationPolicyDecision) Valid() bool {
	if d.Sequence < 1 || !validID(d.WorkspaceID) || !validID(d.PolicyRevisionID) || d.PolicyRevision < 1 ||
		(d.TargetKind != TargetToolVersion && d.TargetKind != TargetToolset) || !validID(d.TargetID) || d.TargetRevision < 1 ||
		!d.RiskLevel.Valid() || (d.Outcome != PolicyAllow && d.Outcome != PolicyDeny) || len(d.ReasonCodes) == 0 || d.EvaluatedAt.IsZero() {
		return false
	}
	for _, code := range d.ReasonCodes {
		if !policyReasonCodes[code] {
			return false
		}
	}
	return true
}
