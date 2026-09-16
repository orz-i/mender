package domain

import "time"

type PlatformStaffRole string

const (
	PlatformSupport  PlatformStaffRole = "support"
	PlatformReviewer PlatformStaffRole = "reviewer"
	PlatformOperator PlatformStaffRole = "operator"
	PlatformAuditor  PlatformStaffRole = "auditor"
)

type PlatformStaff struct {
	UserID    string
	Role      PlatformStaffRole
	Disabled  bool
	CreatedAt time.Time
}

func (r PlatformStaffRole) Valid() bool {
	return r == PlatformSupport || r == PlatformReviewer || r == PlatformOperator || r == PlatformAuditor
}

func (s PlatformStaff) Active() bool {
	return ValidID(s.UserID) && s.Role.Valid() && !s.Disabled && !s.CreatedAt.IsZero()
}

func (s PlatformStaff) Allows(action string) bool {
	if !s.Active() {
		return false
	}
	switch action {
	case "support:request":
		return s.Role == PlatformSupport || s.Role == PlatformOperator
	case "dangerous:review":
		return s.Role == PlatformReviewer || s.Role == PlatformOperator
	case "dangerous:audit":
		return s.Role == PlatformReviewer || s.Role == PlatformOperator || s.Role == PlatformAuditor
	default:
		return false
	}
}
