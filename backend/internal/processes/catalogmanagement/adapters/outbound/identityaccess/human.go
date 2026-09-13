package identityaccess

import (
	"context"
	"errors"

	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
	"github.com/orz-i/mender/backend/internal/processes/catalogmanagement/application"
)

type Human struct{ identity identity.HumanIdentity }

func New(identity identity.HumanIdentity) *Human { return &Human{identity: identity} }

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

func (h *Human) Authenticate(ctx context.Context, raw string) (application.Actor, error) {
	p, err := h.identity.AuthenticateBrowser(ctx, raw)
	return application.Actor{UserID: p.UserID}, mapError(err)
}

func (h *Human) AuthenticateMutation(ctx context.Context, raw, csrf string) (application.Actor, error) {
	p, err := h.identity.AuthenticateBrowserMutation(ctx, raw, csrf)
	return application.Actor{UserID: p.UserID}, mapError(err)
}

func (h *Human) Authorize(ctx context.Context, actor application.Actor, workspace, action string) error {
	return mapError(h.identity.AuthorizeHuman(ctx, identity.HumanPrincipal{UserID: actor.UserID}, workspace, action))
}

var _ application.Authorizer = (*Human)(nil)
