package domain

import "time"

// RunStartDelegation is an exact, short-lived human capability. Unlike a
// machine API Key it cannot select arbitrary tools/connections or raise its
// own charge ceiling.
type RunStartDelegation struct {
	ID, Digest, WorkspaceID, UserID                      string
	ToolsetVersionID, ToolID, ToolVersion, ToolVersionID string
	ConnectionID, Currency                               string
	MaxChargeMicro                                       int64
	IdempotencyKey                                       string
	MembershipRole                                       MembershipRole
	CreatedAt, ExpiresAt, RevokedAt                      time.Time
	UserDisabled, MembershipDisabled, WorkspaceDisabled  bool
}

func validIdempotencyKey(value string) bool {
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if ch < '!' || ch > '~' {
			return false
		}
	}
	return true
}

func validVersion(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for i, ch := range value {
		if i == 0 && !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9') {
			return false
		}
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == ':' || ch == '+' || ch == '-') {
			return false
		}
	}
	return true
}

func (d RunStartDelegation) Validate() bool {
	if !ValidID(d.ID) || len(d.Digest) != 64 || !ValidID(d.WorkspaceID) || !ValidID(d.UserID) || !ValidID(d.ToolsetVersionID) || !ValidID(d.ToolID) || !validVersion(d.ToolVersion) || !ValidID(d.ToolVersionID) || !ValidID(d.ConnectionID) || len(d.Currency) != 3 || d.MaxChargeMicro < 0 || !validIdempotencyKey(d.IdempotencyKey) || d.CreatedAt.IsZero() || !d.ExpiresAt.After(d.CreatedAt) {
		return false
	}
	for _, ch := range d.Currency {
		if ch < 'A' || ch > 'Z' {
			return false
		}
	}
	return d.RevokedAt.IsZero() || !d.RevokedAt.Before(d.CreatedAt)
}

func (d RunStartDelegation) ActiveAt(at time.Time) bool {
	return d.Validate() && d.MembershipRole.Valid() && !d.UserDisabled && !d.MembershipDisabled && !d.WorkspaceDisabled && d.RevokedAt.IsZero() && !at.Before(d.CreatedAt) && at.Before(d.ExpiresAt)
}

func (d RunStartDelegation) AllowsCreate(at time.Time) bool {
	return d.ActiveAt(at) && (d.MembershipRole == RoleOwner || d.MembershipRole == RoleAdmin || d.MembershipRole == RoleDeveloper)
}
