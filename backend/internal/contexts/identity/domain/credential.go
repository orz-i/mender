package domain

import (
	"errors"
	"slices"
	"time"
)

var ErrInvalidCredential = errors.New("invalid credential record")

func ValidID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func ValidScope(scope string) bool { return scope == "run:read" || scope == "run:cancel" }

// Credential is a detached authorization record. It never stores the raw secret.
type Credential struct {
	ID, WorkspaceID, SubjectID, Digest          string
	Scopes                                      []string
	CreatedAt, ExpiresAt                        time.Time
	Revoked, WorkspaceDisabled, SubjectDisabled bool
}

func (c Credential) Validate() error {
	if !ValidID(c.ID) || !ValidID(c.WorkspaceID) || !ValidID(c.SubjectID) || len(c.Digest) != 64 || c.CreatedAt.IsZero() || !c.ExpiresAt.After(c.CreatedAt) || len(c.Scopes) == 0 {
		return ErrInvalidCredential
	}
	for _, b := range c.Digest {
		if !(b >= '0' && b <= '9' || b >= 'a' && b <= 'f') {
			return ErrInvalidCredential
		}
	}
	for _, s := range c.Scopes {
		if !ValidScope(s) {
			return ErrInvalidCredential
		}
	}
	return nil
}

func (c Credential) ActiveAt(at time.Time) bool {
	return c.Validate() == nil && !at.IsZero() && !at.Before(c.CreatedAt) && at.Before(c.ExpiresAt) && !c.Revoked && !c.WorkspaceDisabled && !c.SubjectDisabled
}

func (c Credential) Allows(scope string, at time.Time) bool {
	return c.ActiveAt(at) && ValidScope(scope) && slices.Contains(c.Scopes, scope)
}
