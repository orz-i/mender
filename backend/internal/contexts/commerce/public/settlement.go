package public

import (
	"context"
	"errors"
	"time"
)

var (
	ErrSettlementConflict    = errors.New("usage settlement conflict")
	ErrSettlementUnavailable = errors.New("usage settlement unavailable")
)

type SettlementRequest struct {
	WorkspaceID, RunID, ReservationID, PriceVersionID, BudgetID, PeriodID, Currency string
	ReservedMicro                                                                   int64
	Outcome                                                                         string
	ObservedAt, SettledAt                                                           time.Time
}

type SettlementReceipt struct {
	WorkspaceID, RunID, ReservationID, PriceVersionID, BudgetID, PeriodID, Currency string
	ReservedMicro, ChargedMicro                                                     int64
	Outcome                                                                         string
	ObservedAt, SettledAt                                                           time.Time
	Replay                                                                          bool
}

type Settler interface {
	Settle(context.Context, SettlementRequest) (SettlementReceipt, error)
}
