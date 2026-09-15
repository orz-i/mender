package facade

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	execution "github.com/orz-i/mender/backend/internal/contexts/execution/public"
)

type ProviderCallbacks struct {
	service *application.ProviderCallbackService
}

func NewProviderCallbacks(service *application.ProviderCallbackService) *ProviderCallbacks {
	return &ProviderCallbacks{service: service}
}

func (f *ProviderCallbacks) IngestProviderCallback(ctx context.Context, callback execution.ProviderCallback) (execution.ProviderCallbackReceipt, error) {
	if f == nil || f.service == nil {
		return execution.ProviderCallbackReceipt{}, execution.ErrProviderCallbackUnavailable
	}
	receipt, err := f.service.Ingest(ctx, application.ProviderCallback{
		ProviderID: callback.ProviderID, EventType: callback.EventType, EventID: callback.EventID, BodySHA256: callback.BodySHA256, KeyID: callback.KeyID,
		WorkspaceID: callback.WorkspaceID, RunID: callback.RunID, AttemptNo: callback.AttemptNo,
		ProviderRequestID: callback.ProviderRequestID, ExternalTaskID: callback.ExternalTaskID, ObservationID: callback.ObservationID,
		State: callback.State, ResultJSON: callback.ResultJSON, ErrorCode: callback.ErrorCode,
		InputRequestID: callback.InputRequestID, InputPrompt: callback.InputPrompt, InputSchemaJSON: callback.InputSchemaJSON,
		SignedAt: callback.SignedAt, ReceivedAt: callback.ReceivedAt, ObservedAt: callback.ObservedAt,
	})
	if err != nil {
		switch {
		case errors.Is(err, application.ErrProviderCallbackInvalid):
			return execution.ProviderCallbackReceipt{}, execution.ErrProviderCallbackInvalid
		case errors.Is(err, application.ErrProviderCallbackConflict):
			return execution.ProviderCallbackReceipt{}, execution.ErrProviderCallbackConflict
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return execution.ProviderCallbackReceipt{}, err
		default:
			return execution.ProviderCallbackReceipt{}, execution.ErrProviderCallbackUnavailable
		}
	}
	return execution.ProviderCallbackReceipt{
		ReceiptID: receipt.ReceiptID, ProviderID: receipt.ProviderID, EventID: receipt.EventID,
		WorkspaceID: receipt.WorkspaceID, RunID: receipt.RunID, ObservationID: receipt.ObservationID,
		Disposition: execution.ProviderCallbackDisposition(receipt.Disposition), ReasonCode: receipt.ReasonCode,
		ReceivedAt: receipt.ReceivedAt, ProcessedAt: receipt.ProcessedAt,
	}, nil
}

var _ execution.ProviderCallbacks = (*ProviderCallbacks)(nil)
