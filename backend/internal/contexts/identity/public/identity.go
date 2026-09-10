package public

import (
	"context"
	"errors"
)

var (
	ErrUnauthenticated = errors.New("invalid or inactive credential")
	ErrForbidden       = errors.New("access denied")
	ErrUnavailable     = errors.New("identity unavailable")
)

// Principal is the published, secret-free identity projection, not a request DTO.
type Principal struct{ CredentialID, WorkspaceID, SubjectID string }
type Identity interface {
	Authenticate(context.Context, string) (Principal, error)
	Authorize(context.Context, Principal, string, string) error
}
