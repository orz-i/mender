package public

import (
	"context"
	"errors"
)

var (
	ErrProviderInputUnavailable = errors.New("provider input unavailable")
	ErrProviderInputForbidden   = errors.New("provider input forbidden")
)

type InputDisposition string

const (
	InputAccepted InputDisposition = "accepted"
	InputUnknown  InputDisposition = "unknown"
)

type InputSubmission struct {
	WorkspaceID       string
	RunID             string
	AttemptNo         uint32
	ProviderID        string
	ProviderRequestID string
	ExternalTaskID    string
	InputRequestID    string
	SubmissionID      string
	AnswerJSON        string
}

type InputResult struct {
	Disposition  InputDisposition
	SubmissionID string
}

type ProviderInputSender interface {
	SendInput(context.Context, InputSubmission) (InputResult, error)
}
