package public

import (
	"context"
	"errors"
	"time"
)

var ErrReleaseConflict = errors.New("reservation release conflict")
var ErrReleaseUnavailable = errors.New("reservation release unavailable")

type ReleaseRequest struct {
	WorkspaceID, RunID, ReservationID, BudgetID, PeriodID, Currency string
	AmountMicro                                                     int64
}
type ReleaseState struct {
	Released   bool
	ReleasedAt time.Time
}
type Releaser interface {
	Inspect(context.Context, ReleaseRequest) (ReleaseState, error)
	Release(context.Context, ReleaseRequest, time.Time) error
}
