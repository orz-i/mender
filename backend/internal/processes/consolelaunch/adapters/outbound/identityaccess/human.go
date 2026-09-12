package identityaccess

import (
	"context"
	"errors"

	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
	"github.com/orz-i/mender/backend/internal/processes/consolelaunch/application"
)

type Human struct{ identity identity.HumanIdentity }

func New(identityPort identity.HumanIdentity) *Human { return &Human{identity: identityPort} }

func (h *Human) Authenticate(ctx context.Context, raw string) (application.Actor, error) {
	if h == nil || h.identity == nil {
		return application.Actor{}, application.ErrUnavailable
	}
	principal, err := h.identity.AuthenticateBrowser(ctx, raw)
	if err != nil {
		if errors.Is(err, identity.ErrUnauthenticated) {
			return application.Actor{}, application.ErrUnauthenticated
		}
		return application.Actor{}, application.ErrUnavailable
	}
	return application.Actor{UserID: principal.UserID}, nil
}

func (h *Human) Authorize(ctx context.Context, actor application.Actor, workspace, action string) error {
	if h == nil || h.identity == nil {
		return application.ErrUnavailable
	}
	err := h.identity.AuthorizeHuman(ctx, identity.HumanPrincipal{UserID: actor.UserID}, workspace, action)
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

var _ application.Authorizer = (*Human)(nil)
