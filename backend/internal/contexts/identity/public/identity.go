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

// HumanPrincipal is the secret-free projection used by other bounded contexts.
// Session and CSRF digests remain private to Identity.
type HumanPrincipal struct{ UserID string }

type HumanIdentity interface {
	AuthenticateBrowser(context.Context, string) (HumanPrincipal, error)
	AuthenticateBrowserMutation(context.Context, string, string) (HumanPrincipal, error)
	AuthorizeHuman(context.Context, HumanPrincipal, string, string) error
}
