package execution_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	app "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type executorFunc func(context.Context, app.ExecutorSubmission) (app.ExecutorResult, error)

func (f executorFunc) Submit(ctx context.Context, request app.ExecutorSubmission) (app.ExecutorResult, error) {
	return f(ctx, request)
}

func leasedFixture(t *testing.T, worker string) (*app.WorkerControl, *workerRepoFake, *workerClock, app.Lease) {
	t.Helper()
	control, repo, clock := newWorkerFixture(t)
	if _, err := control.Activate(context.Background(), "ws_a", []string{"deploy_v1"}, 1); err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(time.Second)
	lease, err := control.LeaseOne(context.Background(), "ws_a", worker, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return control, repo, clock, lease
}

func TestDispatcherPersistsIntentBeforeExecutorAndRecordsAcceptance(t *testing.T) {
	control, repo, _, lease := leasedFixture(t, "worker_dispatch")
	called := false
	executor := executorFunc(func(_ context.Context, request app.ExecutorSubmission) (app.ExecutorResult, error) {
		called = true
		if repo.attempt == nil || repo.attempt.Snapshot().State != domain.AttemptSubmitting {
			t.Fatal("executor observed request before durable submission intent")
		}
		if request.SubmissionKey != "mender.submit.run_a.1" || request.RunID != "run_a" || request.Generation != 1 || request.AttemptNo != 1 {
			t.Fatal(request)
		}
		return app.ExecutorResult{Disposition: app.ExecutorAccepted, ProviderRequestID: "provider/request-1", ExternalTaskID: "task-1"}, nil
	})
	dispatcher, err := app.NewDispatcher(control, executor)
	if err != nil {
		t.Fatal(err)
	}
	result, err := dispatcher.Dispatch(context.Background(), lease)
	if err != nil || !called || result.Run.State != domain.Running || result.Attempt.State != domain.AttemptSubmitted || result.SubmissionKey != "mender.submit.run_a.1" {
		t.Fatal(result, called, err)
	}
}

func TestDispatcherMapsUnknownAndExecutorErrorsWithoutPersistingRawError(t *testing.T) {
	for _, mode := range []string{"unknown", "error", "invalid_acceptance"} {
		t.Run(mode, func(t *testing.T) {
			control, repo, _, lease := leasedFixture(t, "worker_"+mode)
			executor := executorFunc(func(context.Context, app.ExecutorSubmission) (app.ExecutorResult, error) {
				switch mode {
				case "unknown":
					return app.ExecutorResult{Disposition: app.ExecutorUnknown}, nil
				case "error":
					return app.ExecutorResult{}, errors.New("raw-secret-like-executor-detail")
				default:
					return app.ExecutorResult{Disposition: app.ExecutorAccepted}, nil
				}
			})
			dispatcher, err := app.NewDispatcher(control, executor)
			if err != nil {
				t.Fatal(err)
			}
			result, err := dispatcher.Dispatch(context.Background(), lease)
			if !errors.Is(err, app.ErrExecutorOutcomeUnknown) || result.Run.State != domain.Reconciling || result.Attempt.State != domain.AttemptUnknown || repo.job.Snapshot().State != domain.JobReconciling {
				t.Fatal(result, repo.job.Snapshot(), err)
			}
			if strings.Contains(result.Attempt.UnknownReason, "raw-secret-like") {
				t.Fatal("raw executor error was persisted", result.Attempt.UnknownReason)
			}
		})
	}
}

func TestDispatcherInvalidOutcomeReconcilesAndSignalsContractError(t *testing.T) {
	control, _, _, lease := leasedFixture(t, "worker_invalid")
	dispatcher, err := app.NewDispatcher(control, executorFunc(func(context.Context, app.ExecutorSubmission) (app.ExecutorResult, error) {
		return app.ExecutorResult{Disposition: "impossible"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	result, err := dispatcher.Dispatch(context.Background(), lease)
	if !errors.Is(err, app.ErrInvalidExecutorResponse) || result.Run.State != domain.Reconciling || result.Attempt.State != domain.AttemptUnknown {
		t.Fatal(result, err)
	}
}

func TestDispatcherRejectsExpiredLeaseBeforeExecutorCall(t *testing.T) {
	control, _, clock, lease := leasedFixture(t, "worker_expired")
	clock.at = lease.LeaseUntil
	called := false
	dispatcher, err := app.NewDispatcher(control, executorFunc(func(context.Context, app.ExecutorSubmission) (app.ExecutorResult, error) {
		called = true
		return app.ExecutorResult{Disposition: app.ExecutorAccepted, ProviderRequestID: "must-not-run"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dispatcher.Dispatch(context.Background(), lease); !errors.Is(err, app.ErrWorkerLeaseLost) || called {
		t.Fatal("expired lease reached executor", err, called)
	}
}

func TestDispatcherRejectsMissingDependencies(t *testing.T) {
	if dispatcher, err := app.NewDispatcher(nil, executorFunc(func(context.Context, app.ExecutorSubmission) (app.ExecutorResult, error) {
		return app.ExecutorResult{}, nil
	})); !errors.Is(err, app.ErrExecutorUnavailable) || dispatcher != nil {
		t.Fatal(dispatcher, err)
	}
}
