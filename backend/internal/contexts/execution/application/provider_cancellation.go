package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var (
	ErrProviderCancelUnavailable    = errors.New("provider cancellation unavailable")
	ErrNoProviderCancellation       = errors.New("no provider cancellation available")
	ErrProviderCancelOutcomeUnknown = errors.New("provider cancellation outcome unknown")
	ErrInvalidProviderCancelResult  = errors.New("invalid provider cancellation result")
)

type ProviderCancelTarget struct {
	WorkspaceID       domain.WorkspaceID
	RunID             domain.RunID
	AttemptNo         uint32
	CancelKey         string
	ProviderID        string
	ProviderRequestID string
	ExternalTaskID    string
	RequestedAt       time.Time
	SendingAt         time.Time
}

func (t ProviderCancelTarget) Valid() bool {
	if !(ProviderTarget{WorkspaceID: t.WorkspaceID, RunID: t.RunID, AttemptNo: t.AttemptNo, ProviderID: t.ProviderID, ProviderRequestID: t.ProviderRequestID, ExternalTaskID: t.ExternalTaskID}).Valid() || len(t.CancelKey) < 8 || len(t.CancelKey) > 200 || t.RequestedAt.IsZero() {
		return false
	}
	for _, ch := range t.CancelKey {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == ':' || ch == '-') {
			return false
		}
	}
	if !t.SendingAt.IsZero() && t.SendingAt.Before(t.RequestedAt) {
		return false
	}
	return true
}

type ProviderCancelRequestRecord struct {
	Run    domain.Snapshot
	Target ProviderCancelTarget
	Replay bool
}

type ProviderCancelRequestRepository interface {
	RequestProviderCancel(context.Context, ports.Caller, domain.RunID, string, time.Time) (ProviderCancelRequestRecord, error)
}

type ProviderCancelClock interface{ Now() time.Time }

type ProviderCancelRequests struct {
	repository ProviderCancelRequestRepository
	clock      ProviderCancelClock
}

func NewProviderCancelRequests(repository ProviderCancelRequestRepository, clock ProviderCancelClock) (*ProviderCancelRequests, error) {
	if repository == nil || clock == nil {
		return nil, ErrProviderCancelUnavailable
	}
	return &ProviderCancelRequests{repository: repository, clock: clock}, nil
}

func (s *ProviderCancelRequests) Request(ctx context.Context, caller ports.Caller, runID domain.RunID, reason string) (ProviderCancelRequestRecord, error) {
	if err := ctx.Err(); err != nil {
		return ProviderCancelRequestRecord{}, err
	}
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	if !caller.WorkspaceID.IsValid() || !runID.IsValid() || strings.TrimSpace(caller.SubjectID) == "" || len(caller.SubjectID) > 512 || strings.TrimSpace(caller.CredentialID) == "" || len(caller.CredentialID) > 512 || len([]rune(reason)) > 500 || strings.ContainsRune(reason, 0) || at.IsZero() || at.Year() < 1 || at.Year() > 9999 {
		return ProviderCancelRequestRecord{}, ErrInvalidProviderCancelResult
	}
	record, err := s.repository.RequestProviderCancel(ctx, caller, runID, reason, at)
	if err != nil {
		return ProviderCancelRequestRecord{}, err
	}
	if record.Run.WorkspaceID != caller.WorkspaceID || record.Run.ID != runID || record.Run.State != domain.CancelRequested || !record.Target.Valid() || record.Target.WorkspaceID != caller.WorkspaceID || record.Target.RunID != runID {
		return ProviderCancelRequestRecord{}, ErrProviderCancelUnavailable
	}
	return record, nil
}

func (s *ProviderCancelRequests) RequestProviderCancellation(ctx context.Context, caller ports.Caller, runID domain.RunID, reason string) (domain.Snapshot, bool, error) {
	record, err := s.Request(ctx, caller, runID, reason)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return domain.Snapshot{}, false, err
		case errors.Is(err, ErrInvalidProviderCancelResult):
			return domain.Snapshot{}, false, ports.ErrUnsafeCancel
		default:
			return domain.Snapshot{}, false, ports.ErrUnavailable
		}
	}
	return record.Run, true, nil
}

var _ ports.ProviderCancelRequester = (*ProviderCancelRequests)(nil)

type ProviderCancelDisposition string

const (
	ProviderCancelAcknowledged ProviderCancelDisposition = "acknowledged"
	ProviderCancelUnconfirmed  ProviderCancelDisposition = "unknown"
)

type ProviderCancelResult struct {
	Disposition   ProviderCancelDisposition
	ObservationID string
	ObservedAt    time.Time
}

type ProviderCancelSource interface {
	CancelProvider(context.Context, ProviderCancelTarget) (ProviderCancelResult, error)
}

type ProviderCancelControlRepository interface {
	ClaimProviderCancel(context.Context, domain.WorkspaceID, time.Time) (ProviderCancelTarget, bool, error)
	RecordProviderCancelUnknown(context.Context, ProviderCancelTarget, time.Time, string) error
	RecordProviderCancelAcknowledged(context.Context, ProviderCancelTarget, string, time.Time) (ProviderResultRecord, error)
}

type ProviderCancelDispatcher struct {
	repository ProviderCancelControlRepository
	source     ProviderCancelSource
	clock      ProviderCancelClock
}

func NewProviderCancelDispatcher(repository ProviderCancelControlRepository, source ProviderCancelSource, clock ProviderCancelClock) (*ProviderCancelDispatcher, error) {
	if repository == nil || source == nil || clock == nil {
		return nil, ErrProviderCancelUnavailable
	}
	return &ProviderCancelDispatcher{repository: repository, source: source, clock: clock}, nil
}

func cancelNow(clock ProviderCancelClock) (time.Time, error) {
	at := clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() || at.Year() < 1 || at.Year() > 9999 {
		return time.Time{}, ErrProviderCancelUnavailable
	}
	return at, nil
}

func (d *ProviderCancelDispatcher) markUnknown(ctx context.Context, target ProviderCancelTarget) error {
	at, err := cancelNow(d.clock)
	if err != nil {
		return err
	}
	if err = d.repository.RecordProviderCancelUnknown(ctx, target, at, "provider cancellation outcome unknown"); err != nil {
		return err
	}
	return ErrProviderCancelOutcomeUnknown
}

func (d *ProviderCancelDispatcher) CancelOne(ctx context.Context, workspace domain.WorkspaceID) (ProviderResultRecord, error) {
	if err := ctx.Err(); err != nil {
		return ProviderResultRecord{}, err
	}
	at, err := cancelNow(d.clock)
	if err != nil {
		return ProviderResultRecord{}, err
	}
	target, found, err := d.repository.ClaimProviderCancel(ctx, workspace, at)
	if err != nil {
		return ProviderResultRecord{}, err
	}
	if !found {
		return ProviderResultRecord{}, ErrNoProviderCancellation
	}
	if !target.Valid() || target.WorkspaceID != workspace || target.SendingAt.IsZero() {
		return ProviderResultRecord{}, ErrInvalidProviderCancelResult
	}
	result, callErr := d.source.CancelProvider(ctx, target)
	if ctx.Err() != nil {
		// The durable sending state means a later cycle must not reissue this
		// cancellation. Provider status reconciliation can still resolve the Run.
		return ProviderResultRecord{}, ctx.Err()
	}
	if callErr != nil || result.Disposition == ProviderCancelUnconfirmed {
		return ProviderResultRecord{}, d.markUnknown(ctx, target)
	}
	if result.Disposition != ProviderCancelAcknowledged || result.ObservationID == "" || result.ObservedAt.IsZero() {
		if err = d.markUnknown(ctx, target); err != nil && !errors.Is(err, ErrProviderCancelOutcomeUnknown) {
			return ProviderResultRecord{}, err
		}
		return ProviderResultRecord{}, ErrInvalidProviderCancelResult
	}
	record, err := d.repository.RecordProviderCancelAcknowledged(ctx, target, result.ObservationID, result.ObservedAt.UTC().Truncate(time.Microsecond))
	if err != nil {
		return ProviderResultRecord{}, err
	}
	return record, nil
}
