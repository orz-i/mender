package public

import (
	"context"
	"errors"
)

var (
	ErrExecutorForbidden   = errors.New("supplier executor forbidden")
	ErrExecutorUnavailable = errors.New("supplier executor unavailable")
)

type Disposition string

const (
	Accepted Disposition = "accepted"
	Unknown  Disposition = "unknown"
)

type Submission struct {
	WorkspaceID, RunID string
	AttemptNo          uint32
	Generation         uint64
	SubmissionKey      string
}

type Result struct {
	Disposition       Disposition
	ProviderRequestID string
	ExternalTaskID    string
}

type Executor interface {
	Submit(context.Context, Submission) (Result, error)
}
