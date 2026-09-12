package domain

import "time"

// RunDelegation is a short-lived, explicit human-to-Run capability. It is not
// a browser session and intentionally cannot carry run:create.
type RunDelegation struct {
	ID, Digest, WorkspaceID, UserID string
	Scopes                          []string
	MembershipRole                  MembershipRole
	CreatedAt, ExpiresAt            time.Time
	RevokedAt                       time.Time
	UserDisabled                    bool
	MembershipDisabled              bool
	WorkspaceDisabled               bool
}

func (d RunDelegation) Validate() bool {
	if !ValidID(d.ID) || len(d.Digest) != 64 || !ValidID(d.WorkspaceID) || !ValidID(d.UserID) || d.CreatedAt.IsZero() || !d.ExpiresAt.After(d.CreatedAt) || len(d.Scopes) < 1 || len(d.Scopes) > 2 {
		return false
	}
	seen := map[string]bool{}
	for _, scope := range d.Scopes {
		if scope != "run:read" && scope != "run:cancel" || seen[scope] {
			return false
		}
		seen[scope] = true
	}
	return d.RevokedAt.IsZero() || !d.RevokedAt.Before(d.CreatedAt)
}

func (d RunDelegation) ActiveAt(at time.Time) bool {
	return d.Validate() && d.MembershipRole.Valid() && !d.UserDisabled && !d.MembershipDisabled && !d.WorkspaceDisabled && d.RevokedAt.IsZero() && !at.Before(d.CreatedAt) && at.Before(d.ExpiresAt)
}

func (d RunDelegation) Allows(scope string, at time.Time) bool {
	if !d.ActiveAt(at) {
		return false
	}
	for _, candidate := range d.Scopes {
		if candidate == scope {
			switch scope {
			case "run:read":
				return true
			case "run:cancel":
				return d.MembershipRole == RoleOwner || d.MembershipRole == RoleAdmin || d.MembershipRole == RoleDeveloper
			}
		}
	}
	return false
}
