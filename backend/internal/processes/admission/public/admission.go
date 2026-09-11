package public

import (
	"context"
	"errors"
)

var (
	ErrInvalid           = errors.New("invalid admission request")
	ErrUnauthenticated   = errors.New("admission requires authentication")
	ErrForbidden         = errors.New("admission forbidden")
	ErrConflict          = errors.New("admission idempotency conflict")
	ErrBudgetExceeded    = errors.New("admission budget exceeded")
	ErrBudgetUnavailable = errors.New("admission budget unavailable")
	ErrUnavailable       = errors.New("admission unavailable")
	ErrCommitUnconfirmed = errors.New("admission commit unconfirmed")
)

// Caller and Request are stable, transport-free process contracts. They do not
// expose database transactions, HTTP request objects or identity internals.
type Caller struct{ WorkspaceID, SubjectID, CredentialID string }

type Request struct {
	IdempotencyKey, ToolID, ToolVersion, ToolsetVersionID, ConnectionID, Currency, MaxChargeMicro string
	Arguments                                                                                     []byte
}

type Receipt struct {
	WorkspaceID, RunID, ReservationID, Currency string
	ReservedMicro                               int64
	Replayed                                    bool
}

type Admission interface {
	Start(context.Context, Caller, Request) (Receipt, error)
}
