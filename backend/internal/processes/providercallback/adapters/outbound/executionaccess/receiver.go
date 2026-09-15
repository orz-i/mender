package executionaccess

import (
	"context"
	"errors"

	execution "github.com/orz-i/mender/backend/internal/contexts/execution/public"
	"github.com/orz-i/mender/backend/internal/processes/providercallback/application"
)

type Receiver struct{ callbacks execution.ProviderCallbacks }

func New(callbacks execution.ProviderCallbacks) *Receiver { return &Receiver{callbacks: callbacks} }

func (r *Receiver) IngestProviderCallback(ctx context.Context, callback application.Callback) (application.Receipt, error) {
	if r == nil || r.callbacks == nil {
		return application.Receipt{}, application.ErrUnavailable
	}
	receipt, err := r.callbacks.IngestProviderCallback(ctx, execution.ProviderCallback{
		ProviderID: callback.ProviderID, EventType: callback.EventType, EventID: callback.EventID, BodySHA256: callback.BodySHA256, KeyID: callback.KeyID,
		WorkspaceID: callback.WorkspaceID, RunID: callback.RunID, AttemptNo: callback.AttemptNo,
		ProviderRequestID: callback.ProviderRequestID, ExternalTaskID: callback.ExternalTaskID, ObservationID: callback.ObservationID,
		State: callback.State, ResultJSON: callback.ResultJSON, ErrorCode: callback.ErrorCode,
		InputRequestID: callback.InputRequestID, InputPrompt: callback.InputPrompt, InputSchemaJSON: callback.InputSchemaJSON,
		SignedAt: callback.SignedAt, ReceivedAt: callback.ReceivedAt, ObservedAt: callback.ObservedAt,
	})
	if err != nil {
		switch {
		case errors.Is(err, execution.ErrProviderCallbackInvalid), errors.Is(err, execution.ErrProviderCallbackConflict):
			return application.Receipt{}, application.ErrInvalid
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return application.Receipt{}, err
		default:
			return application.Receipt{}, application.ErrUnavailable
		}
	}
	return application.Receipt{
		ReceiptID: receipt.ReceiptID, ProviderID: receipt.ProviderID, EventID: receipt.EventID,
		WorkspaceID: receipt.WorkspaceID, RunID: receipt.RunID, ObservationID: receipt.ObservationID,
		Disposition: application.Disposition(receipt.Disposition), ReasonCode: receipt.ReasonCode,
		ReceivedAt: receipt.ReceivedAt, ProcessedAt: receipt.ProcessedAt,
	}, nil
}

var _ application.Receiver = (*Receiver)(nil)
