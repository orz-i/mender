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

// RunDelegationPrincipal is the secret-free projection of an explicit,
// short-lived human delegation. Browser session cookies are not accepted by
// this contract.
type RunDelegationPrincipal struct{ DelegationID, WorkspaceID, UserID string }

type RunDelegations interface {
	AuthenticateRunDelegation(context.Context, string) (RunDelegationPrincipal, error)
	AuthorizeRunDelegation(context.Context, RunDelegationPrincipal, string, string) error
}

type RunStartConstraint struct {
	ToolsetVersionID, ToolID, ToolVersion, ToolVersionID string
	ConnectionID, Currency                               string
	MaxChargeMicro                                       int64
	IdempotencyKey                                       string
}

type RunStartDelegationPrincipal struct {
	DelegationID, WorkspaceID, UserID string
	Constraint                        RunStartConstraint
}

type RunStartDelegations interface {
	AuthenticateRunStartDelegation(context.Context, string) (RunStartDelegationPrincipal, error)
	AuthorizeRunStartDelegation(context.Context, RunStartDelegationPrincipal, string, RunStartConstraint) error
}
