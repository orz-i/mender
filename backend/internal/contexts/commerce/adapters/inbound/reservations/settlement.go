package reservations

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	contract "github.com/orz-i/mender/backend/internal/contexts/commerce/public"
)

type SettlementFacade struct {
	service *application.SettlementService
}

func NewSettlement(service *application.SettlementService) *SettlementFacade {
	return &SettlementFacade{service: service}
}

func (f *SettlementFacade) Settle(ctx context.Context, q contract.SettlementRequest) (contract.SettlementReceipt, error) {
	if f == nil || f.service == nil {
		return contract.SettlementReceipt{}, contract.ErrSettlementUnavailable
	}
	outcome, err := application.ParseSettlementOutcome(q.Outcome)
	if err != nil {
		return contract.SettlementReceipt{}, contract.ErrSettlementConflict
	}
	r, err := f.service.Settle(ctx, application.SettlementRequest{WorkspaceID: q.WorkspaceID, RunID: q.RunID, ReservationID: q.ReservationID, PriceVersionID: q.PriceVersionID, BudgetID: q.BudgetID, PeriodID: q.PeriodID, Currency: q.Currency, ReservedMicro: q.ReservedMicro, Outcome: outcome, ObservedAt: q.ObservedAt, SettledAt: q.SettledAt})
	if err != nil {
		if errors.Is(err, application.ErrSettlementConflict) {
			return contract.SettlementReceipt{}, contract.ErrSettlementConflict
		}
		return contract.SettlementReceipt{}, contract.ErrSettlementUnavailable
	}
	return contract.SettlementReceipt{WorkspaceID: r.WorkspaceID, RunID: r.RunID, ReservationID: r.ReservationID, PriceVersionID: r.PriceVersionID, BudgetID: r.BudgetID, PeriodID: r.PeriodID, Currency: r.Currency, ReservedMicro: r.ReservedMicro, ChargedMicro: r.ChargedMicro, Outcome: string(r.Outcome), ObservedAt: r.ObservedAt, SettledAt: r.SettledAt, Replay: r.Replay}, nil
}

var _ contract.Settler = (*SettlementFacade)(nil)
