package identityaccess

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
)

type Human struct{ identity identity.HumanIdentity }

func NewHuman(identityPort identity.HumanIdentity) *Human { return &Human{identity: identityPort} }

func mapError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, identity.ErrUnauthenticated):
		return application.ErrObservabilityUnauthenticated
	case errors.Is(err, identity.ErrForbidden):
		return application.ErrObservabilityForbidden
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return application.ErrObservabilityUnavailable
	}
}

func (h *Human) Authenticate(ctx context.Context, raw string) (application.HumanUsageActor, error) {
	if h == nil || h.identity == nil {
		return application.HumanUsageActor{}, application.ErrObservabilityUnavailable
	}
	principal, err := h.identity.AuthenticateBrowser(ctx, raw)
	return application.HumanUsageActor{UserID: principal.UserID}, mapError(err)
}

func (h *Human) Authorize(ctx context.Context, actor application.HumanUsageActor, workspace, action string) error {
	if h == nil || h.identity == nil {
		return application.ErrObservabilityUnavailable
	}
	return mapError(h.identity.AuthorizeHuman(ctx, identity.HumanPrincipal{UserID: actor.UserID}, workspace, action))
}

var _ application.UsageAuthorizer = (*Human)(nil)
