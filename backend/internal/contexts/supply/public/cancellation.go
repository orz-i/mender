package public

import (
	"context"
	"errors"
	"time"
)

var ErrProviderCancelUnavailable = errors.New("provider cancellation unavailable")

type CancelDisposition string

const (
	CancelAcknowledged CancelDisposition = "acknowledged"
	CancelUnknown      CancelDisposition = "unknown"
)

type CancelQuery struct {
	ProviderID, ProviderRequestID, ExternalTaskID, CancelKey string
}

type CancelResult struct {
	Disposition   CancelDisposition
	ObservationID string
	ObservedAt    time.Time
}

type ProviderCanceler interface {
	Cancel(context.Context, CancelQuery) (CancelResult, error)
}
