package capabilities

import (
	"context"
	"errors"
	"time"

	commerce "github.com/orz-i/mender/backend/internal/contexts/commerce/public"
	"github.com/orz-i/mender/backend/internal/processes/settlement/application"
)

type Jobs interface {
	Claim(context.Context) (application.Candidate, bool, error)
	Finish(context.Context, application.Candidate, time.Time) error
}

type Scope struct {
	jobs     Jobs
	commerce commerce.Settler
}

func New(jobs Jobs, settler commerce.Settler) (*Scope, error) {
	if jobs == nil || settler == nil {
		return nil, application.ErrUnavailable
	}
	return &Scope{jobs: jobs, commerce: settler}, nil
}
func (s *Scope) Claim(ctx context.Context) (application.Candidate, bool, error) {
	return s.jobs.Claim(ctx)
}
func (s *Scope) Settle(ctx context.Context, c application.Candidate, at time.Time) (application.Receipt, error) {
	r, err := s.commerce.Settle(ctx, commerce.SettlementRequest{WorkspaceID: c.WorkspaceID, RunID: c.RunID, ReservationID: c.ReservationID, PriceVersionID: c.PriceVersionID, BudgetID: c.BudgetID, PeriodID: c.PeriodID, Currency: c.Currency, ReservedMicro: c.ReservedMicro, Outcome: c.Outcome, ObservedAt: c.ObservedAt, SettledAt: at})
	if err != nil {
		if errors.Is(err, commerce.ErrSettlementConflict) {
			return application.Receipt{}, application.ErrInvalid
		}
		return application.Receipt{}, application.ErrUnavailable
	}
	return application.Receipt{ChargedMicro: r.ChargedMicro}, nil
}
func (s *Scope) Finish(ctx context.Context, c application.Candidate, at time.Time) error {
	return s.jobs.Finish(ctx, c, at)
}

var _ application.Scope = (*Scope)(nil)
