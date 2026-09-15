package domain

import "time"

type MembershipRole string

const (
	RoleOwner     MembershipRole = "owner"
	RoleAdmin     MembershipRole = "admin"
	RoleDeveloper MembershipRole = "developer"
	RoleViewer    MembershipRole = "viewer"
)

func (r MembershipRole) Valid() bool {
	return r == RoleOwner || r == RoleAdmin || r == RoleDeveloper || r == RoleViewer
}

type HumanSession struct {
	Digest, UserID, CSRFDigest string
	CreatedAt, ExpiresAt       time.Time
	RevokedAt                  time.Time
	UserDisabled               bool
}

func (s HumanSession) ActiveAt(at time.Time) bool {
	return ValidID(s.UserID) && len(s.Digest) == 64 && len(s.CSRFDigest) == 64 && !s.UserDisabled && s.RevokedAt.IsZero() && !at.Before(s.CreatedAt) && at.Before(s.ExpiresAt)
}

type WorkspaceMembership struct {
	WorkspaceID, UserID string
	Role                MembershipRole
	Disabled            bool
	WorkspaceDisabled   bool
	CreatedAt           time.Time
}

func (m WorkspaceMembership) Active() bool {
	return ValidID(m.WorkspaceID) && ValidID(m.UserID) && m.Role.Valid() && !m.Disabled && !m.WorkspaceDisabled && !m.CreatedAt.IsZero()
}

func (m WorkspaceMembership) Allows(action string) bool {
	if !m.Active() {
		return false
	}
	switch action {
	case "workspace:read", "connection:read", "run:read", "usage:read", "catalog:read", "publisher:read":
		return true
	case "connection:manage":
		return m.Role == RoleOwner || m.Role == RoleAdmin
	case "catalog:review", "catalog:audit", "catalog:policy", "publisher:review", "execution:governance":
		return m.Role == RoleOwner || m.Role == RoleAdmin
	case "run:cancel", "run:input", "run:create", "catalog:manage", "publisher:manage":
		return m.Role == RoleOwner || m.Role == RoleAdmin || m.Role == RoleDeveloper
	default:
		return false
	}
}
