package identityaccess

import (
	"context"
	"errors"

	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

type Human struct{ identity identity.HumanIdentity }

func NewHuman(identity identity.HumanIdentity) *Human { return &Human{identity: identity} }

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, identity.ErrUnauthenticated):
		return application.ErrPublicationUnauthenticated
	case errors.Is(err, identity.ErrForbidden):
		return application.ErrPublicationForbidden
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return application.ErrPublicationUnavailable
	}
}

func (h *Human) Authenticate(ctx context.Context, raw string) (application.PublisherActor, error) {
	p, err := h.identity.AuthenticateBrowser(ctx, raw)
	return application.PublisherActor{UserID: p.UserID}, mapError(err)
}

func (h *Human) AuthenticateMutation(ctx context.Context, raw, csrf string) (application.PublisherActor, error) {
	p, err := h.identity.AuthenticateBrowserMutation(ctx, raw, csrf)
	return application.PublisherActor{UserID: p.UserID}, mapError(err)
}

func (h *Human) Authorize(ctx context.Context, actor application.PublisherActor, workspace, action string) error {
	return mapError(h.identity.AuthorizeHuman(ctx, identity.HumanPrincipal{UserID: actor.UserID}, workspace, action))
}

var _ application.PublisherAuthorizer = (*Human)(nil)
