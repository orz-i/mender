package application

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var (
	ErrNoProviderReconciliation  = errors.New("no provider reconciliation available")
	ErrProviderStatusUnavailable = errors.New("provider status unavailable")
	ErrInvalidProviderStatus     = errors.New("invalid provider status")
)

type ProviderTarget struct {
	WorkspaceID       domain.WorkspaceID
	RunID             domain.RunID
	AttemptNo         uint32
	ProviderID        string
	ProviderRequestID string
	ExternalTaskID    string
	// EvidenceAt is the durable local lower bound for provider observations.
	// A synchronous supplier may finish before the Dispatcher commits the
	// submitted Attempt; reconciliation must never write an observation that
	// predates that local submission evidence. It is optional for validation
	// probes and non-persistent test targets, but PostgreSQL targets populate it.
	EvidenceAt time.Time
}

func (t ProviderTarget) Valid() bool {
	if !t.WorkspaceID.IsValid() || !t.RunID.IsValid() || t.AttemptNo == 0 || t.AttemptNo > 100 || !validControlID(t.ProviderID) || len(t.ProviderRequestID) < 1 || len(t.ProviderRequestID) > 512 || !utf8.ValidString(t.ProviderRequestID) || len(t.ExternalTaskID) > 512 || !utf8.ValidString(t.ExternalTaskID) {
		return false
	}
	for _, value := range []string{t.ProviderRequestID, t.ExternalTaskID} {
		for _, ch := range value {
			if ch < 0x20 || ch == 0x7f {
				return false
			}
		}
	}
	if !t.EvidenceAt.IsZero() && (t.EvidenceAt.Year() < 1 || t.EvidenceAt.Year() > 9999) {
		return false
	}
	return true
}

type ProviderStatus struct {
	ObservationID string
	State         domain.ProviderResultState
	ResultJSON    string
	ErrorCode     string
	InputRequest  *AgentInputRequest
	ObservedAt    time.Time
}

type ProviderReconciliationRepository interface {
	NextProviderTarget(context.Context, domain.WorkspaceID) (ProviderTarget, bool, error)
}

type ProviderStatusSource interface {
	QueryProviderStatus(context.Context, ProviderTarget) (ProviderStatus, error)
}

type ProviderResultSink interface {
	Observe(context.Context, domain.ProviderObservation) (ProviderResultRecord, error)
}

type ProviderReconciler struct {
	targets ProviderReconciliationRepository
	status  ProviderStatusSource
	results ProviderResultSink
}

func NewProviderReconciler(targets ProviderReconciliationRepository, status ProviderStatusSource, results ProviderResultSink) (*ProviderReconciler, error) {
	if targets == nil || status == nil || results == nil {
		return nil, ErrProviderStatusUnavailable
	}
	return &ProviderReconciler{targets: targets, status: status, results: results}, nil
}

func (r *ProviderReconciler) ReconcileOne(ctx context.Context, workspace domain.WorkspaceID) (ProviderResultRecord, error) {
	if err := ctx.Err(); err != nil {
		return ProviderResultRecord{}, err
	}
	if !workspace.IsValid() {
		return ProviderResultRecord{}, ErrInvalidProviderStatus
	}
	target, found, err := r.targets.NextProviderTarget(ctx, workspace)
	if err != nil {
		return ProviderResultRecord{}, err
	}
	if !found {
		return ProviderResultRecord{}, ErrNoProviderReconciliation
	}
	if !target.Valid() || target.WorkspaceID != workspace {
		return ProviderResultRecord{}, ErrInvalidProviderStatus
	}
	status, err := r.status.QueryProviderStatus(ctx, target)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ProviderResultRecord{}, err
		}
		return ProviderResultRecord{}, ErrProviderStatusUnavailable
	}
	if status.InputRequest != nil {
		request := *status.InputRequest
		request.WorkspaceID, request.RunID, request.AttemptNo = target.WorkspaceID, target.RunID, target.AttemptNo
		request.ProviderID, request.ProviderRequestID, request.ExternalTaskID = target.ProviderID, target.ProviderRequestID, target.ExternalTaskID
		if request.RequestedAt.IsZero() {
			request.RequestedAt = status.ObservedAt
		}
		if !target.EvidenceAt.IsZero() && request.RequestedAt.Before(target.EvidenceAt) {
			request.RequestedAt = target.EvidenceAt
		}
		sink, ok := r.results.(ProviderInputRequestSink)
		if !ok || request.Validate() != nil {
			return ProviderResultRecord{}, ErrInvalidProviderStatus
		}
		if _, err = sink.ObserveAgentInputRequest(ctx, request); err != nil {
			return ProviderResultRecord{}, err
		}
		return ProviderResultRecord{}, nil
	}
	observedAt := status.ObservedAt
	if !target.EvidenceAt.IsZero() && observedAt.Before(target.EvidenceAt) {
		// Use durable execution evidence rather than time.Now so replay of the
		// same provider fact remains deterministic. The provider payload and
		// observation identity are untouched; only the local convergence time is
		// bounded by the point at which the submission became durable.
		observedAt = target.EvidenceAt
	}
	observation := domain.ProviderObservation{
		WorkspaceID: target.WorkspaceID, RunID: target.RunID, ObservationID: status.ObservationID,
		AttemptNo: target.AttemptNo, ProviderID: target.ProviderID, ProviderRequestID: target.ProviderRequestID, ExternalTaskID: target.ExternalTaskID,
		State: status.State, ResultJSON: status.ResultJSON, ErrorCode: status.ErrorCode, ObservedAt: observedAt,
	}
	if observation.Validate() != nil {
		return ProviderResultRecord{}, ErrInvalidProviderStatus
	}
	record, err := r.results.Observe(ctx, observation)
	if err != nil {
		return ProviderResultRecord{}, err
	}
	return record, nil
}
