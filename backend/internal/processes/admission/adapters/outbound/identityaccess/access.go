package identityaccess

import (
	"context"
	"errors"

	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
)

type Access struct{ identity identity.Identity }

func New(identityPort identity.Identity) *Access { return &Access{identity: identityPort} }

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, identity.ErrUnauthenticated):
		return application.ErrUnauthenticated
	case errors.Is(err, identity.ErrForbidden):
		return application.ErrForbidden
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return application.ErrUnavailable
	}
}

func (a *Access) Authenticate(ctx context.Context, token string) (application.Caller, error) {
	if a == nil || a.identity == nil {
		return application.Caller{}, application.ErrUnavailable
	}
	p, err := a.identity.Authenticate(ctx, token)
	if err != nil {
		return application.Caller{}, mapError(err)
	}
	return application.Caller{WorkspaceID: p.WorkspaceID, SubjectID: p.SubjectID, CredentialID: p.CredentialID}, nil
}

func (a *Access) Authorize(ctx context.Context, caller application.Caller, _ application.Request) error {
	if a == nil || a.identity == nil {
		return application.ErrUnavailable
	}
	p := identity.Principal{WorkspaceID: caller.WorkspaceID, SubjectID: caller.SubjectID, CredentialID: caller.CredentialID}
	return mapError(a.identity.Authorize(ctx, p, caller.WorkspaceID, "run:create"))
}

var _ application.Authenticator = (*Access)(nil)
var _ application.Authorizer = (*Access)(nil)
