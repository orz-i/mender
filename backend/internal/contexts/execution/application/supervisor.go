package application

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var ErrSupervisorUnavailable = errors.New("dispatch supervisor unavailable")

type SupervisorControl interface {
	Activate(context.Context, domain.WorkspaceID, []string, int) (int, error)
	LeaseOne(context.Context, domain.WorkspaceID, string, time.Duration) (Lease, error)
	Heartbeat(context.Context, Lease, time.Duration) (Lease, error)
	ReleaseBeforeSubmit(context.Context, Lease, time.Duration) error
}

type SupervisorDispatcher interface {
	Dispatch(context.Context, Lease) (DispatchResult, error)
}

type HeartbeatTicker interface {
	C() <-chan time.Time
	Stop()
}

type HeartbeatTickerFactory interface {
	NewTicker(time.Duration) (HeartbeatTicker, error)
}

type SupervisorConfig struct {
	WorkerID          string
	LeaseTTL          time.Duration
	HeartbeatInterval time.Duration
	ActivationLimit   int
}

type DispatchSupervisor struct {
	control    SupervisorControl
	dispatcher SupervisorDispatcher
	tickers    HeartbeatTickerFactory
	config     SupervisorConfig
}

func NewDispatchSupervisor(control SupervisorControl, dispatcher SupervisorDispatcher, tickers HeartbeatTickerFactory, config SupervisorConfig) (*DispatchSupervisor, error) {
	if control == nil || dispatcher == nil || tickers == nil || !validControlID(config.WorkerID) || config.LeaseTTL < 5*time.Second || config.LeaseTTL > 5*time.Minute || config.HeartbeatInterval < 100*time.Millisecond || config.HeartbeatInterval >= config.LeaseTTL/2 || config.ActivationLimit < 1 || config.ActivationLimit > 100 {
		return nil, ErrSupervisorUnavailable
	}
	return &DispatchSupervisor{control: control, dispatcher: dispatcher, tickers: tickers, config: config}, nil
}

type supervisedOutcome struct {
	result DispatchResult
	err    error
}

// DispatchOne activates eligible work, acquires one fenced lease, and keeps the
// lease alive while Dispatcher owns the durable submission boundary. A lost
// heartbeat cancels the dispatch context; once intent exists, normal lease-expiry
// recovery moves the Attempt to reconciliation instead of retrying it.
func (s *DispatchSupervisor) DispatchOne(ctx context.Context, workspace domain.WorkspaceID, revisions []string) (DispatchResult, error) {
	if err := ctx.Err(); err != nil {
		return DispatchResult{}, err
	}
	if !workspace.IsValid() || len(revisions) == 0 {
		return DispatchResult{}, ErrInvalidWorker
	}
	if _, err := s.control.Activate(ctx, workspace, revisions, s.config.ActivationLimit); err != nil {
		return DispatchResult{}, err
	}
	lease, err := s.control.LeaseOne(ctx, workspace, s.config.WorkerID, s.config.LeaseTTL)
	if err != nil {
		return DispatchResult{}, err
	}
	ticker, err := s.tickers.NewTicker(s.config.HeartbeatInterval)
	if err != nil || ticker == nil {
		if ctx.Err() == nil {
			_ = s.control.ReleaseBeforeSubmit(ctx, lease, 0)
		}
		return DispatchResult{}, ErrSupervisorUnavailable
	}
	defer ticker.Stop()
	dispatchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan supervisedOutcome, 1)
	go func() {
		result, dispatchErr := s.dispatcher.Dispatch(dispatchCtx, lease)
		done <- supervisedOutcome{result: result, err: dispatchErr}
	}()
	currentLease := lease
	for {
		select {
		case outcome := <-done:
			return outcome.result, outcome.err
		case <-ticker.C():
			renewed, heartbeatErr := s.control.Heartbeat(dispatchCtx, currentLease, s.config.LeaseTTL)
			if heartbeatErr != nil {
				// Prefer a concurrently completed dispatch outcome when available. If
				// not, cancel network work and let fenced expiry recovery decide state.
				select {
				case outcome := <-done:
					return outcome.result, outcome.err
				default:
				}
				cancel()
				return DispatchResult{}, heartbeatErr
			}
			currentLease = renewed
		case <-ctx.Done():
			cancel()
			return DispatchResult{}, ctx.Err()
		}
	}
}
