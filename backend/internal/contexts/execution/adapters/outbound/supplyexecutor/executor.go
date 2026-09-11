package supplyexecutor

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type Executor struct{ supplier supply.Executor }

func New(supplier supply.Executor) (*Executor, error) {
	if supplier == nil {
		return nil, application.ErrExecutorUnavailable
	}
	return &Executor{supplier: supplier}, nil
}

func (e *Executor) Submit(ctx context.Context, request application.ExecutorSubmission) (application.ExecutorResult, error) {
	result, err := e.supplier.Submit(ctx, supply.Submission{
		WorkspaceID: string(request.WorkspaceID), RunID: string(request.RunID), AttemptNo: request.AttemptNo,
		Generation: request.Generation, SubmissionKey: request.SubmissionKey,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return application.ExecutorResult{}, err
		}
		return application.ExecutorResult{}, application.ErrExecutorUnavailable
	}
	switch result.Disposition {
	case supply.Accepted:
		return application.ExecutorResult{Disposition: application.ExecutorAccepted, ProviderID: result.ProviderID, ProviderRequestID: result.ProviderRequestID, ExternalTaskID: result.ExternalTaskID}, nil
	case supply.Unknown:
		return application.ExecutorResult{Disposition: application.ExecutorUnknown, ProviderID: result.ProviderID, ProviderRequestID: result.ProviderRequestID, ExternalTaskID: result.ExternalTaskID}, nil
	default:
		return application.ExecutorResult{}, nil
	}
}

var _ application.Executor = (*Executor)(nil)
