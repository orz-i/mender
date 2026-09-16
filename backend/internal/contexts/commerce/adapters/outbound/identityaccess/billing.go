package identityaccess

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	identity "github.com/orz-i/mender/backend/internal/contexts/identity/public"
)

type Billing struct{ identity identity.HumanIdentity }

func NewBilling(identityPort identity.HumanIdentity) *Billing {
	return &Billing{identity: identityPort}
}

func billingIdentityError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, identity.ErrUnauthenticated):
		return application.ErrBillingUnauthenticated
	case errors.Is(err, identity.ErrForbidden):
		return application.ErrBillingForbidden
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return application.ErrBillingUnavailable
	}
}

func (b *Billing) AuthenticateBilling(ctx context.Context, raw string) (application.BillingActor, error) {
	if b == nil || b.identity == nil {
		return application.BillingActor{}, application.ErrBillingUnavailable
	}
	p, err := b.identity.AuthenticateBrowser(ctx, raw)
	return application.BillingActor{UserID: p.UserID}, billingIdentityError(err)
}

func (b *Billing) AuthenticateBillingMutation(ctx context.Context, raw, csrf string) (application.BillingActor, error) {
	if b == nil || b.identity == nil {
		return application.BillingActor{}, application.ErrBillingUnavailable
	}
	p, err := b.identity.AuthenticateBrowserMutation(ctx, raw, csrf)
	return application.BillingActor{UserID: p.UserID}, billingIdentityError(err)
}

func (b *Billing) AuthorizeBillingPlatform(ctx context.Context, actor application.BillingActor, action string) error {
	if b == nil || b.identity == nil {
		return application.ErrBillingUnavailable
	}
	return billingIdentityError(b.identity.AuthorizePlatformHuman(ctx, identity.HumanPrincipal{UserID: actor.UserID}, action))
}

var _ application.BillingAuthorizer = (*Billing)(nil)
