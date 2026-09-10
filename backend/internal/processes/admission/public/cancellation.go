package public

import (
	"context"
	"errors"
	"time"
)

var ErrCancelInvalid = errors.New("invalid cancellation")
var ErrCancelForbidden = errors.New("cancellation forbidden")
var ErrCancelUnauthenticated = errors.New("cancellation unauthenticated")
var ErrCancelUnsafe = errors.New("unsafe cancellation")
var ErrCancelUnavailable = errors.New("cancellation unavailable")
var ErrCancelCommitUnconfirmed = errors.New("cancellation commit unconfirmed")

type CancelCaller struct{ WorkspaceID, SubjectID, CredentialID string }
type CancelState struct {
	WorkspaceID, RunID, State string
	Version                   uint64
	CreatedAt, UpdatedAt      time.Time
	Replayed                  bool
}
type CancelResult struct {
	Found bool
	Run   CancelState
}
type Canceler interface {
	Cancel(context.Context, CancelCaller, string, string) (CancelResult, error)
}
