package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	app "github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

func TestProviderWaitingConsumesWorkerLeaseAndOnlyProviderResultFinishes(t *testing.T) {
	at := time.Date(2026, 9, 10, 20, 0, 0, 0, time.UTC)
	job, err := domain.NewBlockedJob("ws_result", "run_result", at, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err = job.Activate(at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	token, err := job.Acquire("worker_result", at.Add(2*time.Second), at.Add(32*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err = job.MarkProviderWaiting(token, at.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	snapshot := job.Snapshot()
	if snapshot.State != domain.JobProviderWaiting || snapshot.LeaseOwner != "" || !snapshot.LeaseUntil.IsZero() || snapshot.LeaseGeneration != 1 {
		t.Fatal("provider waiting retained local execution ownership", snapshot)
	}
	if err = job.Renew(token, at.Add(5*time.Second), at.Add(40*time.Second)); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("released provider-waiting job accepted stale heartbeat", err)
	}
	if err = job.FinishFromProvider(at.Add(6 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if snapshot = job.Snapshot(); snapshot.State != domain.JobFinished || !snapshot.StoppedAt.Equal(at.Add(6*time.Second)) {
		t.Fatal(snapshot)
	}

	queued, err := domain.NewBlockedJob("ws_result", "run_queued", at, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err = queued.Activate(at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = queued.FinishFromProvider(at.Add(2 * time.Second)); !errors.Is(err, domain.ErrJobState) {
		t.Fatal("queued job accepted invented provider terminal result", err)
	}
}

func TestProviderObservationValidation(t *testing.T) {
	at := time.Date(2026, 9, 10, 20, 0, 0, 0, time.UTC)
	base := domain.ProviderObservation{WorkspaceID: "ws_result", RunID: "run_result", ObservationID: "obs_1", AttemptNo: 1, ProviderID: "provider_a", ProviderRequestID: "provider/request-1", ExternalTaskID: "task-1", State: domain.ProviderPending, ObservedAt: at}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	succeeded := base
	succeeded.ObservationID, succeeded.State, succeeded.ResultJSON = "obs_2", domain.ProviderSucceeded, `{"answer":42}`
	if err := succeeded.Validate(); err != nil || !succeeded.IsTerminal() {
		t.Fatal(err, succeeded)
	}
	failed := base
	failed.ObservationID, failed.State, failed.ErrorCode = "obs_3", domain.ProviderFailed, "provider_timeout"
	if err := failed.Validate(); err != nil || !failed.IsTerminal() {
		t.Fatal(err, failed)
	}
	canceled := base
	canceled.ObservationID, canceled.State = "obs_4", domain.ProviderCanceled
	if err := canceled.Validate(); err != nil || !canceled.IsTerminal() {
		t.Fatal(err, canceled)
	}
	for _, invalid := range []domain.ProviderObservation{
		{},
		func() domain.ProviderObservation { v := base; v.ObservationID = "bad observation"; return v }(),
		func() domain.ProviderObservation { v := base; v.ProviderRequestID = ""; return v }(),
		func() domain.ProviderObservation { v := succeeded; v.ResultJSON = `{`; return v }(),
		func() domain.ProviderObservation {
			v := failed
			v.ErrorCode = "raw provider error with spaces"
			return v
		}(),
		func() domain.ProviderObservation { v := canceled; v.ResultJSON = `{}`; return v }(),
	} {
		if err := invalid.Validate(); !errors.Is(err, domain.ErrInvalidProviderObservation) {
			t.Fatal("invalid provider observation accepted", invalid, err)
		}
	}
}

type providerResultRepoFake struct {
	record app.ProviderResultRecord
	calls  int
}

func (r *providerResultRepoFake) RecordProviderObservation(_ context.Context, observation domain.ProviderObservation) (app.ProviderResultRecord, error) {
	r.calls++
	r.record.Observation = observation
	return r.record, nil
}

func TestProviderResultsRejectInvalidObservationBeforeStorage(t *testing.T) {
	repo := &providerResultRepoFake{}
	service, err := app.NewProviderResults(repo)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Observe(context.Background(), domain.ProviderObservation{WorkspaceID: "ws_result", RunID: "run_result"})
	if !errors.Is(err, domain.ErrInvalidProviderObservation) || repo.calls != 0 {
		t.Fatal("invalid observation reached storage", err, repo.calls)
	}
	if service, err = app.NewProviderResults(nil); !errors.Is(err, app.ErrProviderResultUnavailable) || service != nil {
		t.Fatal(service, err)
	}
}
