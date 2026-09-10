package public

import (
	"context"
	"errors"
	"time"
)

var ErrBudgetExceeded = errors.New("budget exceeded")
var ErrBudgetUnavailable = errors.New("budget unavailable")

type ReserveRequest struct {
	WorkspaceID, RunID, ReservationID, BudgetID, PeriodID, Currency string
	AmountMicro                                                     int64
	At                                                              time.Time
}
type Reserver interface {
	Reserve(context.Context, ReserveRequest) error
}
