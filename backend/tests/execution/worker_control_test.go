package execution_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type workerClock struct{ at time.Time }

func (c *workerClock) Now() time.Time { return c.at }

type workerRepoFake struct {
	job             domain.Job
	activateMatches int
	lastRevisions   []string
	recoveries      int
	forceRenewErr   error
}

func (r *workerRepoFake) ActivateOne(_ context.Context, _ domain.WorkspaceID, revisions []string, at time.Time) (bool, error) {
	r.lastRevisions = append([]string(nil), revisions...)
	if r.activateMatches == 0 {
		return false, nil
	}
	r.activateMatches--
	return true, r.job.Activate(at)
}

func (r *workerRepoFake) Acquire(_ context.Context, _ domain.WorkspaceID, worker string, at, until time.Time) (domain.JobSnapshot, error) {
	if _, err := r.job.Acquire(worker, at, until); err != nil {
		return domain.JobSnapshot{}, err
	}
	return r.job.Snapshot(), nil
}

func (r *workerRepoFake) Renew(_ context.Context, token domain.LeaseToken, at, until time.Time) (domain.JobSnapshot, error) {
	if r.forceRenewErr != nil {
		return domain.JobSnapshot{}, r.forceRenewErr
	}
	if err := r.job.Renew(token, at, until); err != nil {
		return domain.JobSnapshot{}, err
	}
	return r.job.Snapshot(), nil
}

func (r *workerRepoFake) ReleaseBeforeSubmit(_ context.Context, token domain.LeaseToken, at, availableAt time.Time) (domain.JobSnapshot, error) {
	if err := r.job.ReleaseBeforeSubmit(token, at, availableAt); err != nil {
		return domain.JobSnapshot{}, err
	}
	return r.job.Snapshot(), nil
}

func (r *workerRepoFake) RecoverExpired(_ context.Context, _ domain.WorkspaceID, at time.Time) (domain.JobSnapshot, bool, error) {
	if r.recoveries == 0 {
		return domain.JobSnapshot{}, false, nil
	}
	r.recoveries--
	if err := r.job.RecoverExpired(at); err != nil {
		return domain.JobSnapshot{}, false, err
	}
	return r.job.Snapshot(), true, nil
}

func newWorkerFixture(t *testing.T) (*application.WorkerControl, *workerRepoFake, *workerClock) {
	t.Helper()
	at := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	job, err := domain.NewBlockedJob("ws_a", "run_a", at, 3)
	if err != nil {
		t.Fatal(err)
	}
	repo := &workerRepoFake{job: job, activateMatches: 1}
	clock := &workerClock{at: at.Add(time.Second)}
	control, err := application.NewWorkerControl(repo, clock)
	if err != nil {
		t.Fatal(err)
	}
	return control, repo, clock
}

func TestWorkerControlActivationLeaseHeartbeatAndRelease(t *testing.T) {
	control, repo, clock := newWorkerFixture(t)
	count, err := control.Activate(context.Background(), "ws_a", []string{"deploy_v1"}, 5)
	if err != nil || count != 1 || len(repo.lastRevisions) != 1 || repo.lastRevisions[0] != "deploy_v1" {
		t.Fatal(count, repo.lastRevisions, err)
	}
	clock.at = clock.at.Add(time.Second)
	lease, err := control.LeaseOne(context.Background(), "ws_a", "worker_a", 30*time.Second)
	if err != nil || lease.Generation != 1 || lease.AttemptNo != 1 || lease.WorkerID != "worker_a" {
		t.Fatal(lease, err)
	}
	clock.at = clock.at.Add(10 * time.Second)
	renewed, err := control.Heartbeat(context.Background(), lease, 30*time.Second)
	if err != nil || renewed.Generation != lease.Generation || !renewed.LeaseUntil.After(lease.LeaseUntil) {
		t.Fatal(renewed, err)
	}
	clock.at = clock.at.Add(time.Second)
	if err = control.ReleaseBeforeSubmit(context.Background(), renewed, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if repo.job.Snapshot().State != domain.JobQueued {
		t.Fatal(repo.job.Snapshot())
	}
}

func TestWorkerControlMapsLeaseLossAndValidatesInputs(t *testing.T) {
	control, repo, clock := newWorkerFixture(t)
	if _, err := control.Activate(context.Background(), "ws_a", []string{"deploy_v1", "deploy_v1"}, 1); !errors.Is(err, application.ErrInvalidWorker) {
		t.Fatal(err)
	}
	if _, err := control.LeaseOne(context.Background(), "ws_a", "bad worker", 30*time.Second); !errors.Is(err, application.ErrInvalidWorker) {
		t.Fatal(err)
	}
	if _, err := control.Activate(context.Background(), "ws_a", []string{"deploy_v1"}, 1); err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(time.Second)
	lease, err := control.LeaseOne(context.Background(), "ws_a", "worker_a", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	repo.forceRenewErr = domain.ErrLeaseLost
	clock.at = clock.at.Add(time.Second)
	if _, err = control.Heartbeat(context.Background(), lease, 30*time.Second); !errors.Is(err, application.ErrWorkerLeaseLost) {
		t.Fatal(err)
	}
}

func TestWorkerControlRecoversExpiredLeaseOnlyAfterExpiry(t *testing.T) {
	control, repo, clock := newWorkerFixture(t)
	if _, err := control.Activate(context.Background(), "ws_a", []string{"deploy_v1"}, 1); err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(time.Second)
	lease, err := control.LeaseOne(context.Background(), "ws_a", "worker_a", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	repo.recoveries = 1
	clock.at = lease.LeaseUntil
	count, err := control.RecoverExpired(context.Background(), "ws_a", 10)
	if err != nil || count != 1 || repo.job.Snapshot().State != domain.JobQueued {
		t.Fatal(count, repo.job.Snapshot(), err)
	}
}
