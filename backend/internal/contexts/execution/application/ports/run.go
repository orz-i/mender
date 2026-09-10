package ports

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var (
	ErrNotFound         = errors.New("run not found")
	ErrConflict         = errors.New("run revision conflict")
	ErrForbidden        = errors.New("run access denied")
	ErrUnauthenticated  = errors.New("invalid machine credential")
	ErrUnavailable      = errors.New("run dependency unavailable")
	ErrAdmissionManaged = errors.New("admitted run requires coordinated cancellation")
)

// Caller must be projected from authenticated server-side identity, never trusted request JSON.
// Defining this port does not implement authentication or tenant authorization.
type Caller struct {
	SubjectID    string
	WorkspaceID  domain.WorkspaceID
	CredentialID string
}

type RunID = domain.RunID
type WorkspaceID = domain.WorkspaceID
type Authenticator interface {
	Authenticate(context.Context, string) (Caller, error)
}

type Change struct {
	Actor  Caller
	Reason string
}

type Action string

const (
	ReadRun       Action = "run:read"
	CancelRun     Action = "run:cancel"
	ListRuns      Action = "run:list"
	ReadRunEvents Action = "run:events:read"
)

type Authorizer interface {
	Authorize(ctx context.Context, caller Caller, action Action, runID domain.RunID) error
}

// Repository is tenant-scoped. Save must atomically compare expectedVersion, preserve
// immutable identity/creation fields, and update only an existing row in that workspace.
// Cancellation and the execution worker must share this concurrency contract.
type Repository interface {
	Find(ctx context.Context, workspaceID domain.WorkspaceID, runID domain.RunID) (domain.Run, error)
	Save(ctx context.Context, run domain.Run, expectedVersion uint64, change Change) error
}

type Clock interface{ Now() time.Time }

// CoordinatedCanceler is optional and externally composed. The bool is true only
// for an admission-managed Run; false permits the existing legacy-only path.
type CoordinatedCanceler interface {
	CancelAdmission(context.Context, Caller, domain.RunID, string) (domain.Snapshot, bool, error)
}

var ErrUnsafeCancel = errors.New("admission is not safely cancelable")
var ErrCancelCommitUnconfirmed = errors.New("cancellation commit unconfirmed")
