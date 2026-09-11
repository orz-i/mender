package application

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalid     = errors.New("invalid settlement process state")
	ErrUnavailable = errors.New("settlement process unavailable")
	ErrNoWork      = errors.New("no settlement work available")
)

type Candidate struct {
	WorkspaceID, RunID, ObservationID, ReservationID, PriceVersionID, BudgetID, PeriodID, Currency string
	ReservedMicro                                                                                  int64
	Outcome                                                                                        string
	ObservedAt                                                                                     time.Time
}
type Receipt struct{ ChargedMicro int64 }
type Scope interface {
	Claim(context.Context) (Candidate, bool, error)
	Settle(context.Context, Candidate, time.Time) (Receipt, error)
	Finish(context.Context, Candidate, time.Time) error
}
type UnitOfWork interface {
	Within(context.Context, string, func(Scope) error) error
}
type Clock interface{ Now() time.Time }
type Service struct {
	uow   UnitOfWork
	clock Clock
}

func New(uow UnitOfWork, clock Clock) (*Service, error) {
	if uow == nil || clock == nil {
		return nil, ErrUnavailable
	}
	return &Service{uow: uow, clock: clock}, nil
}

func validID(v string) bool {
	if len(v) < 1 || len(v) > 200 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == ':') {
			return false
		}
	}
	return true
}

func validCandidate(c Candidate, workspace string) bool {
	if c.WorkspaceID != workspace || c.ReservedMicro < 0 || c.ObservedAt.IsZero() || len(c.Currency) != 3 {
		return false
	}
	for _, v := range []string{c.WorkspaceID, c.RunID, c.ObservationID, c.ReservationID, c.PriceVersionID, c.BudgetID, c.PeriodID} {
		if !validID(v) {
			return false
		}
	}
	switch c.Outcome {
	case "succeeded", "failed", "canceled":
		return true
	}
	return false
}

func (s *Service) SettleOne(ctx context.Context, workspace string) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if !validID(workspace) {
		return Receipt{}, ErrInvalid
	}
	now := s.clock.Now().UTC().Truncate(time.Microsecond)
	if now.IsZero() || now.Year() < 1 || now.Year() > 9999 {
		return Receipt{}, ErrUnavailable
	}
	var out Receipt
	found := false
	err := s.uow.Within(ctx, workspace, func(scope Scope) error {
		candidate, ok, e := scope.Claim(ctx)
		if e != nil {
			return e
		}
		if !ok {
			return nil
		}
		found = true
		if !validCandidate(candidate, workspace) || now.Before(candidate.ObservedAt) {
			return ErrInvalid
		}
		receipt, e := scope.Settle(ctx, candidate, now)
		if e != nil {
			return e
		}
		if receipt.ChargedMicro < 0 || receipt.ChargedMicro > candidate.ReservedMicro {
			return ErrInvalid
		}
		if e = scope.Finish(ctx, candidate, now); e != nil {
			return e
		}
		out = receipt
		return nil
	})
	if err != nil {
		return Receipt{}, err
	}
	if !found {
		return Receipt{}, ErrNoWork
	}
	return out, nil
}
