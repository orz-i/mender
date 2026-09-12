package domain

import "time"

type Summary struct {
	WorkspaceID, ConnectionID, ProviderID string
	State                                 string
	Revision                              int64
	CreatedAt, ExpiresAt                  time.Time
}

func (s Summary) Validate() error {
	if !validID(s.WorkspaceID) || !validID(s.ConnectionID) || !validID(s.ProviderID) || s.Revision < 1 || s.CreatedAt.IsZero() || !s.ExpiresAt.After(s.CreatedAt) {
		return ErrInvalidAccess
	}
	switch s.State {
	case "active", "expired", "revoked", "error":
		return nil
	default:
		return ErrInvalidAccess
	}
}
