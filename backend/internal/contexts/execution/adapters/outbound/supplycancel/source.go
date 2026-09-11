package supplycancel

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	supply "github.com/orz-i/mender/backend/internal/contexts/supply/public"
)

type Source struct {
	cancelers map[string]supply.ProviderCanceler
}

func New(cancelers map[string]supply.ProviderCanceler) (*Source, error) {
	if len(cancelers) == 0 || len(cancelers) > 256 {
		return nil, application.ErrProviderCancelUnavailable
	}
	copyOf := make(map[string]supply.ProviderCanceler, len(cancelers))
	for providerID, canceler := range cancelers {
		probe := application.ProviderCancelTarget{WorkspaceID: "ws_probe", RunID: "run_probe", AttemptNo: 1, CancelKey: "cancel.probe", ProviderID: providerID, ProviderRequestID: "request/probe", RequestedAt: time.Unix(1, 0).UTC()}
		if !probe.Valid() || canceler == nil {
			return nil, application.ErrProviderCancelUnavailable
		}
		copyOf[providerID] = canceler
	}
	return &Source{cancelers: copyOf}, nil
}

func (s *Source) CancelProvider(ctx context.Context, target application.ProviderCancelTarget) (application.ProviderCancelResult, error) {
	if s == nil || !target.Valid() {
		return application.ProviderCancelResult{}, application.ErrInvalidProviderCancelResult
	}
	canceler := s.cancelers[target.ProviderID]
	if canceler == nil {
		return application.ProviderCancelResult{}, application.ErrProviderCancelUnavailable
	}
	result, err := canceler.Cancel(ctx, supply.CancelQuery{ProviderID: target.ProviderID, ProviderRequestID: target.ProviderRequestID, ExternalTaskID: target.ExternalTaskID, CancelKey: target.CancelKey})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return application.ProviderCancelResult{}, err
		}
		return application.ProviderCancelResult{}, application.ErrProviderCancelUnavailable
	}
	switch result.Disposition {
	case supply.CancelAcknowledged:
		return application.ProviderCancelResult{Disposition: application.ProviderCancelAcknowledged, ObservationID: result.ObservationID, ObservedAt: result.ObservedAt}, nil
	case supply.CancelUnknown:
		return application.ProviderCancelResult{Disposition: application.ProviderCancelUnconfirmed}, nil
	default:
		return application.ProviderCancelResult{}, application.ErrInvalidProviderCancelResult
	}
}

var _ application.ProviderCancelSource = (*Source)(nil)
