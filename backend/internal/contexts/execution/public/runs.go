package public

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalid            = errors.New("invalid execution request")
	ErrUnauthenticated    = errors.New("execution requires authentication")
	ErrForbidden          = errors.New("execution forbidden")
	ErrNotFound           = errors.New("execution object not found")
	ErrConflict           = errors.New("execution conflict")
	ErrOutcomeUnconfirmed = errors.New("execution outcome unconfirmed")
	ErrUnavailable        = errors.New("execution unavailable")
)

type Caller struct{ WorkspaceID, SubjectID, CredentialID string }

type Run struct {
	RunID, WorkspaceID, State string
	Version                   uint64
	CreatedAt, UpdatedAt      time.Time
}

type Artifact struct {
	ArtifactID, Kind, MediaType string
	SizeBytes                   int64
	CreatedAt                   time.Time
	ContentJSON                 string
}

type Runs interface {
	GetRun(context.Context, Caller, string) (Run, error)
	CancelRun(context.Context, Caller, string, string) (Run, error)
	GetArtifact(context.Context, Caller, string, string) (Artifact, error)
}
