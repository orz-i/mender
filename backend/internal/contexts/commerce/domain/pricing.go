package domain

import (
	"errors"
	"time"
)

var ErrInvalidPricing = errors.New("invalid pricing state")

type PriceVersion struct {
	ID, ToolVersionID, Currency string
	ReserveMicro                int64
	StartsAt, EndsAt            time.Time
	Active                      bool
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
	return validID(p.ID) && validID(p.ToolVersionID) && validCurrency(p.Currency) && p.ReserveMicro >= 0 && !p.StartsAt.IsZero() && p.EndsAt.After(p.StartsAt) && p.Active && !at.IsZero() && !at.Before(p.StartsAt) && at.Before(p.EndsAt)
}

func (b BudgetWindow) UsableAt(at time.Time) bool {
	return validID(b.WorkspaceID) && validID(b.BudgetID) && validID(b.PeriodID) && validCurrency(b.Currency) && !b.StartsAt.IsZero() && b.EndsAt.After(b.StartsAt) && b.Active && !at.IsZero() && !at.Before(b.StartsAt) && at.Before(b.EndsAt)
}
