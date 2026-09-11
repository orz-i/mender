package domain

import (
	"errors"
	"time"
)

var ErrInvalidPricing = errors.New("invalid pricing state")

type BillingPolicy string
type SettlementOutcome string

const (
	FixedSuccessOnly BillingPolicy = "fixed_success_only"

	OutcomeSucceeded SettlementOutcome = "succeeded"
	OutcomeFailed    SettlementOutcome = "failed"
	OutcomeCanceled  SettlementOutcome = "canceled"
)

type PriceVersion struct {
	ID, ToolVersionID, Currency string
	ReserveMicro, ChargeMicro   int64
	BillingPolicy               BillingPolicy
	StartsAt, EndsAt            time.Time
	Active                      bool
}

func ValidSettlementIdentity(workspace, run, reservation, price, budget, period, currency string) bool {
	return validID(workspace) && validID(run) && validID(reservation) && validID(price) && validID(budget) && validID(period) && validCurrency(currency)
}

type BudgetWindow struct {
	WorkspaceID, BudgetID, PeriodID, Currency string
	StartsAt, EndsAt                          time.Time
	Active                                    bool
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func validCurrency(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, c := range value {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

func (p PriceVersion) UsableAt(at time.Time) bool {
	return p.ValidContract() && p.Active && !at.IsZero() && !at.Before(p.StartsAt) && at.Before(p.EndsAt)
}

func (p PriceVersion) ValidContract() bool {
	return validID(p.ID) && validID(p.ToolVersionID) && validCurrency(p.Currency) && p.ReserveMicro >= 0 && p.ChargeMicro >= 0 && p.ChargeMicro <= p.ReserveMicro && p.BillingPolicy == FixedSuccessOnly && !p.StartsAt.IsZero() && p.EndsAt.After(p.StartsAt)
}

func (p PriceVersion) ChargeFor(outcome SettlementOutcome) (int64, error) {
	if !p.ValidContract() {
		return 0, ErrInvalidPricing
	}
	switch outcome {
	case OutcomeSucceeded:
		return p.ChargeMicro, nil
	case OutcomeFailed, OutcomeCanceled:
		return 0, nil
	default:
		return 0, ErrInvalidPricing
	}
}

func (b BudgetWindow) UsableAt(at time.Time) bool {
	return validID(b.WorkspaceID) && validID(b.BudgetID) && validID(b.PeriodID) && validCurrency(b.Currency) && !b.StartsAt.IsZero() && b.EndsAt.After(b.StartsAt) && b.Active && !at.IsZero() && !at.Before(b.StartsAt) && at.Before(b.EndsAt)
}
