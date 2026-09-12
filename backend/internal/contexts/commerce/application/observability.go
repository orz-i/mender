package application

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrObservabilityUnavailable     = errors.New("commerce observability unavailable")
	ErrObservabilityUnauthenticated = errors.New("commerce observability unauthenticated")
	ErrObservabilityForbidden       = errors.New("commerce observability forbidden")
)

const maxObservabilityItems = 100

type HumanUsageActor struct{ UserID string }

type UsageAuthorizer interface {
	Authenticate(context.Context, string) (HumanUsageActor, error)
	Authorize(context.Context, HumanUsageActor, string, string) error
}

type BudgetPeriodView struct {
	WorkspaceID, BudgetID, PeriodID, Currency string
	StartsAt, EndsAt                          time.Time
	Active                                    bool
	LimitMicro, ConsumedMicro, ReservedMicro  int64
	Revision                                  int64
}

func (v BudgetPeriodView) AvailableMicro() int64 {
	return v.LimitMicro - v.ConsumedMicro - v.ReservedMicro
}

type UsageEntryView struct {
	WorkspaceID, RunID, BudgetID, PeriodID, Currency string
	QuotaState, Outcome                              string
	ReservedMicro                                    int64
	ChargedMicro                                     *int64
	CreatedAt, FinalizedAt                           time.Time
}

func (v UsageEntryView) ReleasedMicro() int64 {
	switch v.QuotaState {
	case "released":
		return v.ReservedMicro
	case "settled":
		if v.ChargedMicro != nil {
			return v.ReservedMicro - *v.ChargedMicro
		}
	}
	return 0
}

type UsageSnapshot struct {
	BudgetPeriods []BudgetPeriodView
	Entries       []UsageEntryView
}

type UsageRepository interface {
	Snapshot(context.Context, string, int) (UsageSnapshot, error)
}

type UsageService struct {
	repository UsageRepository
	authorizer UsageAuthorizer
}

func NewUsageService(repository UsageRepository, authorizer UsageAuthorizer) (*UsageService, error) {
	if repository == nil || authorizer == nil {
		return nil, ErrObservabilityUnavailable
	}
	return &UsageService{repository: repository, authorizer: authorizer}, nil
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func validCurrency(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, ch := range value {
		if ch < 'A' || ch > 'Z' {
			return false
		}
	}
	return true
}

func validateBudget(v BudgetPeriodView, workspace string) bool {
	return v.WorkspaceID == workspace && validID(v.BudgetID) && validID(v.PeriodID) && validCurrency(v.Currency) &&
		!v.StartsAt.IsZero() && v.EndsAt.After(v.StartsAt) && v.LimitMicro >= 0 && v.ConsumedMicro >= 0 &&
		v.ReservedMicro >= 0 && v.ConsumedMicro <= v.LimitMicro && v.ReservedMicro <= v.LimitMicro-v.ConsumedMicro && v.Revision > 0
}

func validateUsage(v UsageEntryView, workspace string) bool {
	if v.WorkspaceID != workspace || !validID(v.RunID) || !validID(v.BudgetID) || !validID(v.PeriodID) || !validCurrency(v.Currency) ||
		v.ReservedMicro < 0 || v.CreatedAt.IsZero() {
		return false
	}
	switch v.QuotaState {
	case "held":
		return v.ChargedMicro == nil && v.Outcome == "" && v.FinalizedAt.IsZero()
	case "released":
		return v.ChargedMicro == nil && v.Outcome == "" && !v.FinalizedAt.IsZero() && !v.FinalizedAt.Before(v.CreatedAt)
	case "settled":
		return v.ChargedMicro != nil && *v.ChargedMicro >= 0 && *v.ChargedMicro <= v.ReservedMicro &&
			(v.Outcome == "succeeded" || v.Outcome == "failed" || v.Outcome == "canceled") && !v.FinalizedAt.IsZero() && !v.FinalizedAt.Before(v.CreatedAt)
	default:
		return false
	}
}

func (s *UsageService) Snapshot(ctx context.Context, actor HumanUsageActor, workspace string) (UsageSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return UsageSnapshot{}, err
	}
	if !validID(workspace) || !validID(actor.UserID) || strings.TrimSpace(actor.UserID) == "" {
		return UsageSnapshot{}, ErrObservabilityForbidden
	}
	if err := s.authorizer.Authorize(ctx, actor, workspace, "usage:read"); err != nil {
		return UsageSnapshot{}, err
	}
	result, err := s.repository.Snapshot(ctx, workspace, maxObservabilityItems)
	if err != nil {
		return UsageSnapshot{}, err
	}
	if err = ctx.Err(); err != nil {
		return UsageSnapshot{}, err
	}
	if len(result.BudgetPeriods) > maxObservabilityItems || len(result.Entries) > maxObservabilityItems {
		return UsageSnapshot{}, ErrObservabilityUnavailable
	}
	for _, period := range result.BudgetPeriods {
		if !validateBudget(period, workspace) {
			return UsageSnapshot{}, ErrObservabilityUnavailable
		}
	}
	for _, entry := range result.Entries {
		if !validateUsage(entry, workspace) {
			return UsageSnapshot{}, ErrObservabilityUnavailable
		}
	}
	return result, nil
}
