package identitycancel

import (
	"context"
	"errors"
	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
	"github.com/orz-i/mender/backend/internal/processes/admission/application"
)

type Access struct{ identity identity.Identity }

func New(i identity.Identity) *Access { return &Access{i} }
func (a *Access) AuthorizeCancellation(ctx context.Context, c application.Caller, _ string) error {
	if a.identity == nil {
		return application.ErrUnavailable
	}
	err := a.identity.Authorize(ctx, identity.Principal{WorkspaceID: c.WorkspaceID, SubjectID: c.SubjectID, CredentialID: c.CredentialID}, c.WorkspaceID, "run:cancel")
	switch {
	case err == nil:
		return nil
	case errors.Is(err, identity.ErrUnauthenticated):
		return application.ErrCancelUnauthenticated
	case errors.Is(err, identity.ErrForbidden):
		return application.ErrForbidden
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return application.ErrUnavailable
	}
}

var _ application.CancelAuthorizer = (*Access)(nil)
