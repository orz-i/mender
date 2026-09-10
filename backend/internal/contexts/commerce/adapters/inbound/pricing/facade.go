package pricing

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	commerce "github.com/orz-i/mender/backend/internal/contexts/commerce/public"
)

type Facade struct{ service *application.PricingService }

func New(service *application.PricingService) *Facade { return &Facade{service: service} }

func (f *Facade) ResolveAdmissionTerms(ctx context.Context, workspace, budgetID, priceVersionID, toolVersionID, currency string, at time.Time) (commerce.Terms, error) {
	t, err := f.service.Resolve(ctx, workspace, budgetID, priceVersionID, toolVersionID, currency, at)
	if err != nil {
		switch {
		case errors.Is(err, application.ErrPriceUnavailable):
			return commerce.Terms{}, commerce.ErrPriceUnavailable
		case errors.Is(err, application.ErrPricingBudget):
			return commerce.Terms{}, commerce.ErrPricingBudget
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return commerce.Terms{}, err
		default:
			return commerce.Terms{}, commerce.ErrPricingUnavailable
		}
	}
	return commerce.Terms{PriceVersionID: t.PriceVersionID, BudgetID: t.BudgetID, PeriodID: t.PeriodID, Currency: t.Currency, ReserveMicro: t.ReserveMicro, ValidUntil: t.ValidUntil}, nil
}

var _ commerce.Pricing = (*Facade)(nil)
