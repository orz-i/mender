package public

import (
	"context"
	"errors"
	"time"
)

var (
	ErrProviderStatusUnavailable = errors.New("provider status unavailable")
	ErrProviderStatusNotFound    = errors.New("provider status not found")
)

type ProviderStatusState string

const (
	StatusPending   ProviderStatusState = "pending"
	StatusSucceeded ProviderStatusState = "succeeded"
	StatusFailed    ProviderStatusState = "failed"
	StatusCanceled  ProviderStatusState = "canceled"
)

type StatusQuery struct {
	ProviderID, ProviderRequestID, ExternalTaskID string
}

type StatusObservation struct {
	ObservationID string
	State         ProviderStatusState
	ResultJSON    string
	ErrorCode     string
	ObservedAt    time.Time
}

type ProviderStatusReader interface {
	QueryStatus(context.Context, StatusQuery) (StatusObservation, error)
}
