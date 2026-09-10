package application

import (
	"context"
	"errors"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/domain"
	"time"
)

var ErrBudgetUnavailable = errors.New("budget period unavailable")
var ErrBudgetExceeded = errors.New("budget exceeded")
var ErrReservationUnavailable = errors.New("reservation storage unavailable")

type ReserveRequest struct {
	WorkspaceID, RunID, ReservationID, BudgetID, PeriodID, Currency string
	AmountMicro                                                     int64
	At                                                              time.Time
}
type BudgetPeriod struct {
	WorkspaceID, BudgetID, PeriodID, Currency string
	StartsAt, EndsAt, CheckedAt               time.Time
	Active                                    bool
	Allowance                                 domain.Allowance
}

// Repository instances are transaction-scoped. LoadForUpdate and StoreReservation use
// the caller's single transaction; neither commits or calls an external service.
type ReservationRepository interface {
	LoadForUpdate(context.Context, ReserveRequest) (BudgetPeriod, error)
	StoreReservation(context.Context, ReserveRequest, domain.Allowance, int64) error
}
type ReservationService struct{ repo ReservationRepository }

func NewReservationService(repo ReservationRepository) (*ReservationService, error) {
	if repo == nil {
		return nil, ErrReservationUnavailable
	}
	return &ReservationService{repo: repo}, nil
}
func (s *ReservationService) Reserve(ctx context.Context, req ReserveRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if req.AmountMicro < 0 || req.At.IsZero() || req.WorkspaceID == "" || req.RunID == "" || req.ReservationID == "" || req.BudgetID == "" || req.PeriodID == "" {
		return ErrBudgetUnavailable
	}
	p, err := s.repo.LoadForUpdate(ctx, req)
	if err != nil {
		return err
	}
	if p.WorkspaceID != req.WorkspaceID || p.BudgetID != req.BudgetID || p.PeriodID != req.PeriodID || p.Currency != req.Currency || !p.Active || p.CheckedAt.IsZero() || req.At.Before(p.StartsAt) || !req.At.Before(p.EndsAt) || p.CheckedAt.Before(p.StartsAt) || !p.CheckedAt.Before(p.EndsAt) {
		return ErrBudgetUnavailable
	}
	expected := p.Allowance.Snapshot().Revision
	if err = p.Allowance.Reserve(req.AmountMicro); err != nil {
		if errors.Is(err, domain.ErrLimitExceeded) {
			return ErrBudgetExceeded
		}
		return ErrBudgetUnavailable
	}
	return s.repo.StoreReservation(ctx, req, p.Allowance, expected)
}
