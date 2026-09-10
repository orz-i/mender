package domain

import (
	"errors"
	"time"
)

var ErrInvalidAccess = errors.New("invalid connection access")

type Access struct {
	WorkspaceID, ConnectionID, ProviderID, SubjectID string
	State                                            string
	Revision                                         int64
	ConnectionCreatedAt, ConnectionExpiresAt         time.Time
	GrantCreatedAt, GrantExpiresAt                   time.Time
	GrantActive                                      bool
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

func (a Access) Validate() error {
	if !validID(a.WorkspaceID) || !validID(a.ConnectionID) || !validID(a.ProviderID) || !validID(a.SubjectID) || a.Revision < 1 || a.ConnectionCreatedAt.IsZero() || !a.ConnectionExpiresAt.After(a.ConnectionCreatedAt) || a.GrantCreatedAt.IsZero() || !a.GrantExpiresAt.After(a.GrantCreatedAt) {
		return ErrInvalidAccess
	}
	switch a.State {
	case "active", "expired", "revoked", "error":
		return nil
	default:
		return ErrInvalidAccess
	}
}

func (a Access) AllowedAt(at time.Time) bool {
	return a.Validate() == nil && a.State == "active" && a.GrantActive && !at.IsZero() && !at.Before(a.ConnectionCreatedAt) && at.Before(a.ConnectionExpiresAt) && !at.Before(a.GrantCreatedAt) && at.Before(a.GrantExpiresAt)
}

func (a Access) ValidUntil() time.Time {
	if a.GrantExpiresAt.Before(a.ConnectionExpiresAt) {
		return a.GrantExpiresAt
	}
	return a.ConnectionExpiresAt
}
