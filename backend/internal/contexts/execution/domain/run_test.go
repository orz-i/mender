package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

var epoch = time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)

func queued(t *testing.T) domain.Run {
	t.Helper()
	r, err := domain.NewQueuedRun("run_1", "ws_1", epoch)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestStateTransitionMatrix(t *testing.T) {
	states := []domain.State{domain.Queued, domain.Running, domain.WaitingInput, domain.CancelRequested, domain.Reconciling, domain.Succeeded, domain.Failed, domain.Canceled, domain.TimedOut}
	makeState := func(t *testing.T, state domain.State) domain.Run {
		t.Helper()
		r := queued(t)
		var err error
		if state == domain.TimedOut {
			err = r.TimeoutBeforeStart(epoch)
		} else if state != domain.Queued {
			err = r.Start(epoch)
			if err == nil {
				switch state {
				case domain.WaitingInput:
					err = r.WaitForInput(epoch)
				case domain.CancelRequested:
					_, err = r.RequestCancel(epoch)
				case domain.Reconciling:
					err = r.MarkOutcomeUnconfirmed(epoch)
				case domain.Succeeded:
					err = r.ConfirmSucceeded(epoch)
				case domain.Failed:
					err = r.ConfirmFailed(epoch)
				case domain.Canceled:
					_, err = r.RequestCancel(epoch)
					if err == nil {
						err = r.ConfirmCanceled(epoch)
					}
				}
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	for _, command := range []struct {
		name     string
		apply    func(*domain.Run, time.Time) error
		accepted map[domain.State]domain.State
	}{
		{"start", (*domain.Run).Start, map[domain.State]domain.State{domain.Queued: domain.Running}},
		{"wait", (*domain.Run).WaitForInput, map[domain.State]domain.State{domain.Running: domain.WaitingInput}},
		{"resume", (*domain.Run).Resume, map[domain.State]domain.State{domain.WaitingInput: domain.Running}},
		{"cancel", func(r *domain.Run, at time.Time) error { _, err := r.RequestCancel(at); return err }, map[domain.State]domain.State{domain.Queued: domain.Canceled, domain.Running: domain.CancelRequested, domain.WaitingInput: domain.CancelRequested, domain.CancelRequested: domain.CancelRequested, domain.Succeeded: domain.Succeeded, domain.Failed: domain.Failed, domain.Canceled: domain.Canceled, domain.TimedOut: domain.TimedOut}},
		{"unknown", (*domain.Run).MarkOutcomeUnconfirmed, map[domain.State]domain.State{domain.Running: domain.Reconciling, domain.WaitingInput: domain.Reconciling, domain.CancelRequested: domain.Reconciling, domain.Reconciling: domain.Reconciling}},
		{"success", (*domain.Run).ConfirmSucceeded, map[domain.State]domain.State{domain.Running: domain.Succeeded, domain.WaitingInput: domain.Succeeded, domain.CancelRequested: domain.Succeeded, domain.Reconciling: domain.Succeeded, domain.Succeeded: domain.Succeeded}},
		{"failure", (*domain.Run).ConfirmFailed, map[domain.State]domain.State{domain.Running: domain.Failed, domain.WaitingInput: domain.Failed, domain.CancelRequested: domain.Failed, domain.Reconciling: domain.Failed, domain.Failed: domain.Failed}},
		{"canceled", (*domain.Run).ConfirmCanceled, map[domain.State]domain.State{domain.CancelRequested: domain.Canceled, domain.Reconciling: domain.Canceled, domain.Canceled: domain.Canceled}},
		{"timeout_before_start", (*domain.Run).TimeoutBeforeStart, map[domain.State]domain.State{domain.Queued: domain.TimedOut}},
	} {
		for _, state := range states {
			t.Run(command.name+"/"+string(state), func(t *testing.T) {
				r := makeState(t, state)
				before := r.Snapshot()
				err := command.apply(&r, epoch.Add(time.Second))
				next, allowed := command.accepted[state]
				if !allowed {
					if err == nil || r.Snapshot() != before {
						t.Fatalf("rejected transition changed aggregate: %v", err)
					}
					return
				}
				if err != nil || r.Snapshot().State != next {
					t.Fatalf("wanted %s, got %+v: %v", next, r.Snapshot(), err)
				}
				if next == state {
					if r.Snapshot() != before {
						t.Fatal("idempotent transition changed revision or timestamp")
					}
				} else if r.Snapshot().Version != before.Version+1 || r.Snapshot().CreatedAt != before.CreatedAt {
					t.Fatal("transition violated revision or creation invariant")
				}
			})
		}
	}
}

func TestCreationAndIdentifiers(t *testing.T) {
	r := queued(t)
	s := r.Snapshot()
	if s.State != domain.Queued || s.Version != 1 || !s.CreatedAt.Equal(epoch) {
		t.Fatalf("unexpected snapshot: %+v", s)
	}
	s.State = domain.Succeeded
	if r.Snapshot().State != domain.Queued {
		t.Fatal("snapshot mutates aggregate")
	}
	for _, id := range []string{"", " ", "run/1", "run\x00", strings.Repeat("a", 129)} {
		if _, err := domain.NewQueuedRun(domain.RunID(id), "ws_1", epoch); !errors.Is(err, domain.ErrInvalidIdentity) {
			t.Errorf("invalid ID %q accepted", id)
		}
		if _, err := domain.NewQueuedRun("run_1", domain.WorkspaceID(id), epoch); !errors.Is(err, domain.ErrInvalidIdentity) {
			t.Errorf("invalid workspace %q accepted", id)
		}
	}
	if _, err := domain.NewQueuedRun("run_1", "ws_1", time.Time{}); !errors.Is(err, domain.ErrInvalidTime) {
		t.Fatal(err)
	}
}

func TestLifecycleAndDetachedRevisions(t *testing.T) {
	r := queued(t)
	for i, op := range []func(time.Time) error{r.Start, r.WaitForInput, r.Resume, r.ConfirmSucceeded} {
		if err := op(epoch.Add(time.Duration(i+1) * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	s := r.Snapshot()
	if s.State != domain.Succeeded || s.Version != 5 || !s.CreatedAt.Equal(epoch) {
		t.Fatalf("unexpected lifecycle: %+v", s)
	}
	if err := r.ConfirmSucceeded(epoch.Add(5 * time.Second)); err != nil || r.Snapshot().Version != 5 {
		t.Fatal("duplicate completion must not rewrite state")
	}
	if err := r.ConfirmFailed(epoch.Add(6 * time.Second)); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("terminal outcome cannot be overwritten")
	}
}

func TestCancellationDoesNotInventUpstreamConfirmation(t *testing.T) {
	t.Run("queued cancellation is local and idempotent", func(t *testing.T) {
		r := queued(t)
		if changed, err := r.RequestCancel(epoch); !changed || err != nil {
			t.Fatal(changed, err)
		}
		if r.Snapshot().State != domain.Canceled {
			t.Fatal("unsubmitted work can be canceled")
		}
		if changed, err := r.RequestCancel(epoch); changed || err != nil || r.Snapshot().Version != 2 {
			t.Fatal("duplicate cancellation changed revision")
		}
		if err := r.Start(epoch); !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatal("canceled task started")
		}
	})
	for _, waiting := range []bool{false, true} {
		t.Run(map[bool]string{false: "running", true: "waiting_input"}[waiting], func(t *testing.T) {
			r := queued(t)
			if err := r.Start(epoch); err != nil {
				t.Fatal(err)
			}
			if waiting {
				if err := r.WaitForInput(epoch); err != nil {
					t.Fatal(err)
				}
			}
			if changed, err := r.RequestCancel(epoch); !changed || err != nil {
				t.Fatal(changed, err)
			}
			if r.Snapshot().State != domain.CancelRequested {
				t.Fatal("cancel intent is not upstream confirmation")
			}
			version := r.Snapshot().Version
			if changed, err := r.RequestCancel(epoch); changed || err != nil || r.Snapshot().Version != version {
				t.Fatal("request must be idempotent")
			}
			if err := r.ConfirmSucceeded(epoch); err != nil {
				t.Fatal("completion may win the cancellation race", err)
			}
		})
	}
}

func TestUnknownOutcomeRequiresReconciliation(t *testing.T) {
	r := queued(t)
	if err := r.Start(epoch); err != nil {
		t.Fatal(err)
	}
	if err := r.TimeoutBeforeStart(epoch); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("remote timeout cannot be treated as stopped")
	}
	if err := r.MarkOutcomeUnconfirmed(epoch); err != nil {
		t.Fatal(err)
	}
	before := r.Snapshot()
	if changed, err := r.RequestCancel(epoch); changed || !errors.Is(err, domain.ErrOutcomeUnconfirmed) {
		t.Fatal(changed, err)
	}
	if err := r.Start(epoch); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatal("unknown run cannot be restarted")
	}
	if r.Snapshot() != before {
		t.Fatal("invalid transition mutated run")
	}
	if err := r.ConfirmCanceled(epoch); err != nil {
		t.Fatal(err)
	}
}

func TestRejectedCommandsDoNotMutateAggregate(t *testing.T) {
	r := queued(t)
	before := r.Snapshot()
	for _, op := range []func(time.Time) error{r.Resume, r.WaitForInput, r.ConfirmSucceeded, r.ConfirmCanceled, r.MarkOutcomeUnconfirmed} {
		if err := op(epoch); !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatal("invalid queued transition accepted", err)
		}
		if r.Snapshot() != before {
			t.Fatal("rejected command changed aggregate")
		}
	}
	if err := r.Start(epoch.Add(-time.Second)); !errors.Is(err, domain.ErrInvalidTime) {
		t.Fatal(err)
	}
	if err := r.Start(time.Time{}); !errors.Is(err, domain.ErrInvalidTime) {
		t.Fatal(err)
	}
	if r.Snapshot() != before {
		t.Fatal("invalid time changed aggregate")
	}
	var zero domain.Run
	if err := zero.Start(epoch); !errors.Is(err, domain.ErrUninitialized) {
		t.Fatal(err)
	}
}

func TestConfirmedFailureAndPreSubmissionTimeout(t *testing.T) {
	r := queued(t)
	if err := r.Start(epoch); err != nil {
		t.Fatal(err)
	}
	if err := r.ConfirmFailed(epoch); err != nil {
		t.Fatal(err)
	}
	if changed, err := r.RequestCancel(epoch); changed || err != nil {
		t.Fatal("terminal failure changed")
	}
	r = queued(t)
	if err := r.TimeoutBeforeStart(epoch); err != nil || r.Snapshot().State != domain.TimedOut {
		t.Fatal(err)
	}
	if changed, err := r.RequestCancel(epoch); changed || err != nil {
		t.Fatal("terminal timeout changed")
	}
}
