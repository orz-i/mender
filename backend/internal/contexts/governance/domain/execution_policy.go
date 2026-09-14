package domain

import "time"

type ExecutionOutcome string

const (
	ExecutionAllow                ExecutionOutcome = "allow"
	ExecutionConfirmationRequired ExecutionOutcome = "confirmation_required"
	ExecutionDeny                 ExecutionOutcome = "deny"
)

type ExecutionPolicyRevision struct {
	WorkspaceID, ID, State, MaxUnconfirmedRiskLevel, MaxMachineRiskLevel string
	Revision                                                             int64
	DenyUnsafeWrite                                                      bool
	ConfirmationTTLSeconds                                               int
	CreatedByUserID, ActivatedByUserID                                   string
	CreatedAt, ActivatedAt, RetiredAt                                    time.Time
}

func (p ExecutionPolicyRevision) Valid() bool {
	if !validID(p.WorkspaceID) || !validID(p.ID) || p.Revision < 1 || !validRisk(p.MaxUnconfirmedRiskLevel) || !validRisk(p.MaxMachineRiskLevel) ||
		p.ConfirmationTTLSeconds < 30 || p.ConfirmationTTLSeconds > 600 || riskRank(p.MaxMachineRiskLevel) > riskRank(p.MaxUnconfirmedRiskLevel) || p.CreatedAt.IsZero() {
		return false
	}
	switch p.State {
	case "draft":
		return p.ActivatedAt.IsZero() && p.RetiredAt.IsZero()
	case "active":
		return !p.ActivatedAt.IsZero() && p.RetiredAt.IsZero() && !p.ActivatedAt.Before(p.CreatedAt)
	case "retired":
		return !p.ActivatedAt.IsZero() && !p.RetiredAt.IsZero() && !p.ActivatedAt.Before(p.CreatedAt) && !p.RetiredAt.Before(p.ActivatedAt)
	default:
		return false
	}
}

type ExecutionPolicyDecision struct {
	WorkspaceID, PolicyRevisionID, SubjectKind, SubjectID string
	ToolsetVersionID, ToolVersionID, ConnectionID         string
	ArgumentsHash, IdempotencyKeyHash                     string
	RiskLevel                                             string
	Outcome                                               ExecutionOutcome
	ReasonCodes                                           []string
	Sequence, PolicyRevision                              int64
	EvaluatedAt                                           time.Time
}

func (d ExecutionPolicyDecision) Valid() bool {
	if d.Sequence < 1 || d.PolicyRevision < 1 || !validID(d.WorkspaceID) || !validID(d.PolicyRevisionID) || !validID(d.SubjectID) ||
		!validID(d.ToolsetVersionID) || !validID(d.ToolVersionID) || !validID(d.ConnectionID) || !validHash(d.ArgumentsHash) ||
		!validHash(d.IdempotencyKeyHash) || !validRisk(d.RiskLevel) || d.EvaluatedAt.IsZero() || len(d.ReasonCodes) == 0 {
		return false
	}
	if d.SubjectKind != "human" && d.SubjectKind != "machine" {
		return false
	}
	return d.Outcome == ExecutionAllow || d.Outcome == ExecutionConfirmationRequired || d.Outcome == ExecutionDeny
}

type ExecutionConfirmation struct {
	WorkspaceID, ID, UserID, PolicyRevisionID           string
	ToolsetVersionID, ToolVersionID, ConnectionID       string
	ArgumentsHash, IdempotencyKeyHash, RiskLevel, State string
	PolicyRevision                                      int64
	CreatedAt, ExpiresAt, ConsumedAt, ExpiredAt         time.Time
}

func (c ExecutionConfirmation) Valid() bool {
	if !validID(c.WorkspaceID) || !validID(c.ID) || !validID(c.UserID) || !validID(c.PolicyRevisionID) || c.PolicyRevision < 1 ||
		!validID(c.ToolsetVersionID) || !validID(c.ToolVersionID) || !validID(c.ConnectionID) || !validHash(c.ArgumentsHash) ||
		!validHash(c.IdempotencyKeyHash) || !validRisk(c.RiskLevel) || c.CreatedAt.IsZero() || !c.ExpiresAt.After(c.CreatedAt) {
		return false
	}
	switch c.State {
	case "active":
		return c.ConsumedAt.IsZero() && c.ExpiredAt.IsZero()
	case "consumed":
		return !c.ConsumedAt.IsZero() && c.ExpiredAt.IsZero() && !c.ConsumedAt.Before(c.CreatedAt)
	case "expired":
		return c.ConsumedAt.IsZero() && !c.ExpiredAt.IsZero() && !c.ExpiredAt.Before(c.CreatedAt)
	default:
		return false
	}
}

func validHash(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, ch := range v {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func validRisk(v string) bool { return v == "low" || v == "medium" || v == "high" || v == "critical" }

func riskRank(v string) int {
	switch v {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	case "critical":
		return 4
	default:
		return 100
	}
}
