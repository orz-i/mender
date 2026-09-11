package application

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/domain"
)

var (
	ErrSettlementUnavailable = errors.New("usage settlement unavailable")
	ErrSettlementConflict    = errors.New("usage settlement conflicts with commerce state")
)

type SettlementRequest struct {
	WorkspaceID, RunID, ReservationID, PriceVersionID, BudgetID, PeriodID, Currency string
	ReservedMicro                                                                   int64
	Outcome                                                                         domain.SettlementOutcome
	ObservedAt, SettledAt                                                           time.Time
}

type SettlementReceipt struct {
	WorkspaceID, RunID, ReservationID, PriceVersionID, BudgetID, PeriodID, Currency string
	ReservedMicro, ChargedMicro                                                     int64
	Outcome                                                                         domain.SettlementOutcome
	ObservedAt, SettledAt                                                           time.Time
	Replay                                                                          bool
}

type LockedSettlement struct {
	Price             domain.PriceVersion
	Allowance         domain.Allowance
	AllowanceRevision int64
	ReservationState  string
	ReservationAmount int64
	ReservationAt     time.Time
	Existing          *SettlementReceipt
}

type SettlementRepository interface {
	LockSettlement(context.Context, SettlementRequest) (LockedSettlement, error)
	StoreSettlement(context.Context, SettlementRequest, domain.Allowance, int64, int64) (SettlementReceipt, error)
}

type SettlementService struct{ repo SettlementRepository }

func ParseSettlementOutcome(value string) (domain.SettlementOutcome, error) {
	outcome := domain.SettlementOutcome(value)
	switch outcome {
	case domain.OutcomeSucceeded, domain.OutcomeFailed, domain.OutcomeCanceled:
		return outcome, nil
	default:
		return "", ErrSettlementConflict
	}
}

func NewSettlementService(repo SettlementRepository) (*SettlementService, error) {
	if repo == nil {
		return nil, ErrSettlementUnavailable
	}
	return &SettlementService{repo: repo}, nil
}

func sameSettlement(r SettlementReceipt, q SettlementRequest) bool {
	return r.WorkspaceID == q.WorkspaceID && r.RunID == q.RunID && r.ReservationID == q.ReservationID && r.PriceVersionID == q.PriceVersionID && r.BudgetID == q.BudgetID && r.PeriodID == q.PeriodID && r.Currency == q.Currency && r.ReservedMicro == q.ReservedMicro && r.Outcome == q.Outcome && r.ObservedAt.Equal(q.ObservedAt)
}

func (s *SettlementService) Settle(ctx context.Context, q SettlementRequest) (SettlementReceipt, error) {
	if err := ctx.Err(); err != nil {
		return SettlementReceipt{}, err
	}
	if !domain.ValidSettlementIdentity(q.WorkspaceID, q.RunID, q.ReservationID, q.PriceVersionID, q.BudgetID, q.PeriodID, q.Currency) || q.ReservedMicro < 0 || q.ObservedAt.IsZero() || q.SettledAt.Before(q.ObservedAt) || q.SettledAt.Year() < 1 || q.SettledAt.Year() > 9999 {
		return SettlementReceipt{}, ErrSettlementConflict
	}
	locked, err := s.repo.LockSettlement(ctx, q)
	if err != nil {
		return SettlementReceipt{}, err
	}
	if locked.Existing != nil {
		if !sameSettlement(*locked.Existing, q) {
			return SettlementReceipt{}, ErrSettlementConflict
		}
		replay := *locked.Existing
		replay.Replay = true
		return replay, nil
	}
	if locked.ReservationState != "held" || locked.ReservationAmount != q.ReservedMicro || q.ObservedAt.Before(locked.ReservationAt) || q.SettledAt.Before(locked.ReservationAt) || locked.Price.ID != q.PriceVersionID || locked.Price.Currency != q.Currency || locked.Price.ReserveMicro != q.ReservedMicro {
		return SettlementReceipt{}, ErrSettlementConflict
	}
	charged, err := locked.Price.ChargeFor(q.Outcome)
	if err != nil {
		return SettlementReceipt{}, ErrSettlementConflict
	}
	allowance := locked.Allowance
	if err = allowance.Settle(q.ReservedMicro, charged); err != nil {
		return SettlementReceipt{}, ErrSettlementConflict
	}
	return s.repo.StoreSettlement(ctx, q, allowance, locked.AllowanceRevision, charged)
}
