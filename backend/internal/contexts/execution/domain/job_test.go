package domain

import (
	"errors"
	"testing"
	"time"
)

func TestJobLeaseLifecycleUsesMonotonicFencing(t *testing.T) {
	at := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	job, err := NewBlockedJob("ws_a", "run_a", at, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err = job.Activate(at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	first, err := job.Acquire("worker_a", at.Add(2*time.Second), at.Add(32*time.Second))
	if err != nil || first.Generation != 1 {
		t.Fatal(first, err)
	}
	if err = job.Renew(first, at.Add(10*time.Second), at.Add(50*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = job.ReleaseBeforeSubmit(first, at.Add(20*time.Second), at.Add(21*time.Second)); err != nil {
		t.Fatal(err)
	}
	second, err := job.Acquire("worker_b", at.Add(22*time.Second), at.Add(52*time.Second))
	if err != nil || second.Generation != 2 {
		t.Fatal(second, err)
	}
	if err = job.Renew(first, at.Add(23*time.Second), at.Add(60*time.Second)); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("stale token renewed newer lease", err)
	}
	if err = job.RecoverExpired(at.Add(52 * time.Second)); err != nil {
		t.Fatal(err)
	}
	s := job.Snapshot()
	if s.State != JobQueued || s.LeaseOwner != "" || !s.LeaseUntil.IsZero() || s.LeaseGeneration != 2 || s.AttemptCount != 2 {
		t.Fatal("bad recovered state", s)
	}
}

func TestJobRefusesEarlyRecoveryAndExhaustionReblocks(t *testing.T) {
	at := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	job, err := NewBlockedJob("ws_a", "run_a", at, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = job.Activate(at); err != nil {
		t.Fatal(err)
	}
	token, err := job.Acquire("worker_a", at, at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err = job.RecoverExpired(at.Add(59 * time.Second)); !errors.Is(err, ErrLeaseExpired) {
		t.Fatal(err)
	}
	if err = job.ReleaseBeforeSubmit(token, at.Add(time.Minute), at.Add(time.Minute)); !errors.Is(err, ErrLeaseExpired) {
		t.Fatal(err)
	}
	if err = job.RecoverExpired(at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	s := job.Snapshot()
	if s.State != JobBlocked || s.BlockedReason != BlockAttemptLimitReached {
		t.Fatal(s)
	}
	if _, err = job.Acquire("worker_a", at.Add(2*time.Minute), at.Add(3*time.Minute)); !errors.Is(err, ErrJobState) {
		t.Fatal(err)
	}
}

func TestAttemptValidationMatchesLeaseFacts(t *testing.T) {
	at := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	valid := AttemptSnapshot{WorkspaceID: "ws_a", RunID: "run_a", AttemptNo: 1, LeaseGeneration: 1, LeaseOwner: "worker_a", State: AttemptLeased, LeasedAt: at, LeaseUntil: at.Add(time.Minute)}
	if err := ValidateAttempt(valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.LeaseGeneration = 2
	if err := ValidateAttempt(invalid); !errors.Is(err, ErrInvalidJob) {
		t.Fatal(err)
	}
	released := valid
	released.State = AttemptReleased
	released.FinishedAt = at.Add(10 * time.Second)
	if err := ValidateAttempt(released); err != nil {
		t.Fatal(err)
	}
}

func TestCancelNeverLeasedAllowsBlockedOrActivatedQueuedOnly(t *testing.T) {
	at := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	blocked, err := NewBlockedJob("ws_a", "run_cancel_blocked", at, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err = blocked.CancelNeverLeased(at.Add(time.Second)); err != nil || blocked.Snapshot().State != JobCanceled {
		t.Fatal(err, blocked.Snapshot())
	}

	queued, err := NewBlockedJob("ws_a", "run_cancel_queued", at, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err = queued.Activate(at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = queued.CancelNeverLeased(at.Add(2 * time.Second)); err != nil || queued.Snapshot().State != JobCanceled || queued.Snapshot().BlockedReason != "" {
		t.Fatal(err, queued.Snapshot())
	}

	previouslyLeased, err := NewBlockedJob("ws_a", "run_cancel_released", at, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err = previouslyLeased.Activate(at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	token, err := previouslyLeased.Acquire("worker_a", at.Add(2*time.Second), at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err = previouslyLeased.ReleaseBeforeSubmit(token, at.Add(3*time.Second), at.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = previouslyLeased.CancelNeverLeased(at.Add(5 * time.Second)); !errors.Is(err, ErrJobState) {
		t.Fatal("previously leased job accepted for safe release", err)
	}
}
