package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var (
	ErrExecutorUnavailable     = errors.New("executor is unavailable")
	ErrExecutorOutcomeUnknown  = errors.New("supplier outcome is unconfirmed")
	ErrInvalidExecutorResponse = errors.New("executor returned an invalid response")
)

type ExecutorDisposition string

const (
	ExecutorAccepted ExecutorDisposition = "accepted"
	ExecutorUnknown  ExecutorDisposition = "unknown"
)

// ExecutorSubmission is deliberately control-plane-only. A future supplier
// adapter must resolve payloads and credentials through separately reviewed
// ports; execution does not expose those facts through this seam.
type ExecutorSubmission struct {
	WorkspaceID   domain.WorkspaceID
	RunID         domain.RunID
	AttemptNo     uint32
	Generation    uint64
	SubmissionKey string
}

type ExecutorResult struct {
	Disposition       ExecutorDisposition
	ProviderID        string
	ProviderRequestID string
	ExternalTaskID    string
}

type Executor interface {
	Submit(context.Context, ExecutorSubmission) (ExecutorResult, error)
}

type SubmissionControl interface {
	BeginSubmission(context.Context, Lease, string) (SubmissionIntent, error)
	RecordSubmitted(context.Context, SubmissionIntent, string, string, string) (SubmissionRecord, error)
	RecordSubmissionUnknown(context.Context, SubmissionIntent, string, string, string, string) (SubmissionRecord, error)
}

type DispatchResult struct {
	SubmissionKey string
	Run           domain.Snapshot
	Attempt       domain.AttemptSnapshot
}

type Dispatcher struct {
	control  SubmissionControl
	executor Executor
}

func NewDispatcher(control SubmissionControl, executor Executor) (*Dispatcher, error) {
	if control == nil || executor == nil {
		return nil, ErrExecutorUnavailable
	}
	return &Dispatcher{control: control, executor: executor}, nil
}

func deterministicSubmissionKey(lease Lease) (string, error) {
	if !validLeaseIdentity(lease) {
		return "", ErrInvalidWorker
	}
	key := fmt.Sprintf("mender.submit.%s.%d", lease.RunID, lease.Generation)
	if len(key) > 200 {
		return "", ErrInvalidWorker
	}
	return key, nil
}

func (d *Dispatcher) reconcile(ctx context.Context, intent SubmissionIntent, outcome ExecutorResult, reason string) (DispatchResult, error) {
	record, err := d.control.RecordSubmissionUnknown(ctx, intent, outcome.ProviderID, outcome.ProviderRequestID, outcome.ExternalTaskID, reason)
	if err != nil {
		return DispatchResult{}, err
	}
	return DispatchResult{SubmissionKey: intent.Key, Run: record.Run, Attempt: record.Attempt}, ErrExecutorOutcomeUnknown
}

// Dispatch executes exactly one already-leased Attempt. Lease acquisition and
// heartbeat ownership remain outside this seam. The durable intent is always
// written before Executor.Submit can observe the request.
func (d *Dispatcher) Dispatch(ctx context.Context, lease Lease) (DispatchResult, error) {
	if err := ctx.Err(); err != nil {
		return DispatchResult{}, err
	}
	key, err := deterministicSubmissionKey(lease)
	if err != nil {
		return DispatchResult{}, err
	}
	intent, err := d.control.BeginSubmission(ctx, lease, key)
	if err != nil {
		return DispatchResult{}, err
	}
	outcome, submitErr := d.executor.Submit(ctx, ExecutorSubmission{
		WorkspaceID: lease.WorkspaceID, RunID: lease.RunID, AttemptNo: lease.AttemptNo,
		Generation: lease.Generation, SubmissionKey: key,
	})
	if ctx.Err() != nil {
		// Intent is durable. Lease expiry recovery will move it to reconciliation;
		// never invent a second context and risk racing a replacement owner.
		return DispatchResult{SubmissionKey: key}, ctx.Err()
	}
	if submitErr != nil {
		return d.reconcile(ctx, intent, ExecutorResult{}, "executor returned without a confirmed supplier outcome")
	}
	switch outcome.Disposition {
	case ExecutorUnknown:
		return d.reconcile(ctx, intent, outcome, "executor reported supplier outcome unknown")
	case ExecutorAccepted:
		record, recordErr := d.control.RecordSubmitted(ctx, intent, outcome.ProviderID, outcome.ProviderRequestID, outcome.ExternalTaskID)
		if recordErr == nil {
			return DispatchResult{SubmissionKey: key, Run: record.Run, Attempt: record.Attempt}, nil
		}
		if errors.Is(recordErr, ErrWorkerLeaseLost) || errors.Is(recordErr, context.Canceled) || errors.Is(recordErr, context.DeadlineExceeded) {
			return DispatchResult{SubmissionKey: key}, recordErr
		}
		// If acceptance metadata cannot be safely persisted but the lease is still
		// ours, degrade to reconciliation instead of pretending the call failed.
		return d.reconcile(ctx, intent, outcome, "supplier acceptance could not be persisted safely")
	default:
		result, reconcileErr := d.reconcile(ctx, intent, ExecutorResult{}, "executor returned an invalid supplier outcome")
		if reconcileErr != nil && !errors.Is(reconcileErr, ErrExecutorOutcomeUnknown) {
			return DispatchResult{}, reconcileErr
		}
		return result, ErrInvalidExecutorResponse
	}
}
