package supplystatus

import (
	"context"
	"errors"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type Source struct {
	readers map[string]supply.ProviderStatusReader
}

func New(readers map[string]supply.ProviderStatusReader) (*Source, error) {
	if len(readers) == 0 || len(readers) > 256 {
		return nil, application.ErrProviderStatusUnavailable
	}
	copyOf := make(map[string]supply.ProviderStatusReader, len(readers))
	for providerID, reader := range readers {
		target := application.ProviderTarget{WorkspaceID: "ws_probe", RunID: "run_probe", AttemptNo: 1, ProviderID: providerID, ProviderRequestID: "probe"}
		if !target.Valid() || reader == nil {
			return nil, application.ErrProviderStatusUnavailable
		}
		copyOf[providerID] = reader
	}
	return &Source{readers: copyOf}, nil
}

func (s *Source) QueryProviderStatus(ctx context.Context, target application.ProviderTarget) (application.ProviderStatus, error) {
	if s == nil || !target.Valid() {
		return application.ProviderStatus{}, application.ErrInvalidProviderStatus
	}
	reader := s.readers[target.ProviderID]
	if reader == nil {
		return application.ProviderStatus{}, application.ErrProviderStatusUnavailable
	}
	value, err := reader.QueryStatus(ctx, supply.StatusQuery{ProviderID: target.ProviderID, ProviderRequestID: target.ProviderRequestID, ExternalTaskID: target.ExternalTaskID})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return application.ProviderStatus{}, err
		}
		return application.ProviderStatus{}, application.ErrProviderStatusUnavailable
	}
	var state domain.ProviderResultState
	switch value.State {
	case supply.StatusPending:
		state = domain.ProviderPending
	case supply.StatusSucceeded:
		state = domain.ProviderSucceeded
	case supply.StatusFailed:
		state = domain.ProviderFailed
	case supply.StatusCanceled:
		state = domain.ProviderCanceled
	default:
		return application.ProviderStatus{}, application.ErrInvalidProviderStatus
	}
	return application.ProviderStatus{ObservationID: value.ObservationID, State: state, ResultJSON: value.ResultJSON, ErrorCode: value.ErrorCode, ObservedAt: value.ObservedAt}, nil
}

var _ application.ProviderStatusSource = (*Source)(nil)
