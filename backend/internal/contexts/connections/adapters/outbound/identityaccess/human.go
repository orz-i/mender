package identityaccess

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/connections/application"
	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
)

type Human struct{ identity identity.HumanIdentity }

func NewHuman(identity identity.HumanIdentity) *Human { return &Human{identity: identity} }

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

func (h *Human) Authenticate(ctx context.Context, raw string) (application.HumanActor, error) {
	principal, err := h.identity.AuthenticateBrowser(ctx, raw)
	return application.HumanActor{UserID: principal.UserID}, mapError(err)
}

func (h *Human) AuthenticateMutation(ctx context.Context, raw, csrf string) (application.HumanActor, error) {
	principal, err := h.identity.AuthenticateBrowserMutation(ctx, raw, csrf)
	return application.HumanActor{UserID: principal.UserID}, mapError(err)
}

func (h *Human) Authorize(ctx context.Context, actor application.HumanActor, workspace, action string) error {
	return mapError(h.identity.AuthorizeHuman(ctx, identity.HumanPrincipal{UserID: actor.UserID}, workspace, action))
}

var _ application.HumanAuthorizer = (*Human)(nil)
