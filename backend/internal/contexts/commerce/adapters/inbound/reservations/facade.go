package reservations

import (
	"context"
	"errors"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	contract "github.com/orz-i/mender/backend/internal/contexts/commerce/public"
)

type Facade struct {
	service *application.ReservationService
}

func New(service *application.ReservationService) *Facade { return &Facade{service: service} }
func (f *Facade) Reserve(ctx context.Context, r contract.ReserveRequest) error {
	err := f.service.Reserve(ctx, application.ReserveRequest(r))
	if errors.Is(err, application.ErrBudgetExceeded) {
		return contract.ErrBudgetExceeded
	}
	if errors.Is(err, application.ErrBudgetUnavailable) {
		return contract.ErrBudgetUnavailable
	}
	return err
}

var _ contract.Reserver = (*Facade)(nil)
