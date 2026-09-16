package facade

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type ProviderAdmissionGate struct {
	service *application.ProviderAdmission
}

func NewProviderAdmissionGate(service *application.ProviderAdmission) *ProviderAdmissionGate {
	return &ProviderAdmissionGate{service: service}
}

func (g *ProviderAdmissionGate) EnsureProviderAvailable(ctx context.Context, provider string) error {
	if g == nil || g.service == nil {
		return supply.ErrProviderAdmissionUnavailable
	}
	err := g.service.EnsureAvailable(ctx, provider)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, application.ErrProviderQuarantined):
		return supply.ErrProviderQuarantined
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return supply.ErrProviderAdmissionUnavailable
	}
}

var _ supply.ProviderAdmissionGate = (*ProviderAdmissionGate)(nil)
