package public

import (
	"context"
	"errors"
	"time"
)

var ErrUnsafeCancellation = errors.New("admitted run cannot be canceled safely")
var ErrCancellationUnavailable = errors.New("cancellation unavailable")

type CancellationRef struct {
	WorkspaceID, RunID, ReservationID, BudgetID, PeriodID, Currency string
	AmountMicro                                                     int64
}
type CancellationChange struct {
	SubjectID, CredentialID, Reason string
	At                              time.Time
}
type CancellationState struct {
	WorkspaceID, RunID, State string
	Version                   uint64
	CreatedAt, UpdatedAt      time.Time
	Replayed                  bool
}
type Cancellation interface {
	FindReference(context.Context, string, string) (CancellationRef, bool, error)
	Inspect(context.Context, CancellationRef) (CancellationState, error)
	Cancel(context.Context, CancellationRef, CancellationChange) (CancellationState, error)
}
