package public

import (
	"context"
	"errors"
	"time"
)

var (
	ErrPriceUnavailable   = errors.New("price version unavailable")
	ErrPricingBudget      = errors.New("budget window unavailable")
	ErrPricingUnavailable = errors.New("pricing unavailable")
)

type Terms struct {
	PriceVersionID, BudgetID, PeriodID, Currency string
	ReserveMicro                                 int64
	ValidUntil                                   time.Time
}

type Pricing interface {
	ResolveAdmissionTerms(context.Context, string, string, string, string, string, time.Time) (Terms, error)
}
