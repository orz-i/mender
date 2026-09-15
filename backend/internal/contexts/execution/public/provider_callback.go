package public

import (
	"context"
	"errors"
	"time"
)

var (
	ErrProviderCallbackInvalid     = errors.New("invalid provider callback")
	ErrProviderCallbackConflict    = errors.New("provider callback conflict")
	ErrProviderCallbackUnavailable = errors.New("provider callback unavailable")
)

type ProviderCallback struct {
	ProviderID, EventType, EventID, BodySHA256, KeyID, WorkspaceID, RunID string
	AttemptNo                                                             uint32
	ProviderRequestID, ExternalTaskID, ObservationID                      string
	State, ResultJSON, ErrorCode                                          string
	InputRequestID, InputPrompt, InputSchemaJSON                          string
	SignedAt, ReceivedAt, ObservedAt                                      time.Time
}

type ProviderCallbackDisposition string

const (
	ProviderCallbackAccepted    ProviderCallbackDisposition = "accepted"
	ProviderCallbackQuarantined ProviderCallbackDisposition = "quarantined"
	ProviderCallbackDuplicate   ProviderCallbackDisposition = "duplicate"
)

type ProviderCallbackReceipt struct {
	ReceiptID, ProviderID, EventID, WorkspaceID, RunID, ObservationID string
	Disposition                                                       ProviderCallbackDisposition
	ReasonCode                                                        string
	ReceivedAt, ProcessedAt                                           time.Time
}

type ProviderCallbacks interface {
	IngestProviderCallback(context.Context, ProviderCallback) (ProviderCallbackReceipt, error)
}
