package application

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/domain"
)

var (
	ErrPriceUnavailable   = errors.New("price version unavailable")
	ErrPricingBudget      = errors.New("pricing budget unavailable")
	ErrPricingUnavailable = errors.New("pricing dependency unavailable")
)

type PricingRepository interface {
	FindPrice(context.Context, string) (domain.PriceVersion, error)
	FindBudgetWindow(context.Context, string, string, string, time.Time) (domain.BudgetWindow, error)
}

type Terms struct {
	PriceVersionID, BudgetID, PeriodID, Currency string
	ReserveMicro                                 int64
	ValidUntil                                   time.Time
}

type PricingService struct{ repository PricingRepository }

func NewPricingService(repository PricingRepository) (*PricingService, error) {
	if repository == nil {
		return nil, ErrPricingUnavailable
	}
	return &PricingService{repository: repository}, nil
}

func (s *PricingService) Resolve(ctx context.Context, workspace, budgetID, priceVersionID, toolVersionID, currency string, at time.Time) (Terms, error) {
	if err := ctx.Err(); err != nil {
		return Terms{}, err
	}
	p, err := s.repository.FindPrice(ctx, priceVersionID)
	if err != nil {
		return Terms{}, err
	}
	if !p.UsableAt(at) || p.ID != priceVersionID || p.ToolVersionID != toolVersionID || p.Currency != currency {
		return Terms{}, ErrPriceUnavailable
	}
	b, err := s.repository.FindBudgetWindow(ctx, workspace, budgetID, currency, at)
	if err != nil {
		return Terms{}, err
	}
	if !b.UsableAt(at) || b.WorkspaceID != workspace || b.BudgetID != budgetID || b.Currency != currency {
		return Terms{}, ErrPricingBudget
	}
	validUntil := p.EndsAt
	if b.EndsAt.Before(validUntil) {
		validUntil = b.EndsAt
	}
	return Terms{PriceVersionID: p.ID, BudgetID: b.BudgetID, PeriodID: b.PeriodID, Currency: p.Currency, ReserveMicro: p.ReserveMicro, ValidUntil: validUntil}, nil
}
