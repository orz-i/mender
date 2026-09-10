package domain

import (
	"errors"
	"time"
)

type RunID string
type WorkspaceID string

func validID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (id RunID) IsValid() bool       { return validID(string(id)) }
func (id WorkspaceID) IsValid() bool { return validID(string(id)) }

type State string

const (
	Queued          State = "queued"
	Running         State = "running"
	WaitingInput    State = "waiting_input"
	CancelRequested State = "cancel_requested"
	Reconciling     State = "reconciling"
	Succeeded       State = "succeeded"
	Failed          State = "failed"
	Canceled        State = "canceled"
	TimedOut        State = "timed_out"
)

func (s State) IsTerminal() bool {
	return s == Succeeded || s == Failed || s == Canceled || s == TimedOut
}

var (
	ErrInvalidIdentity    = errors.New("invalid run or workspace identifier")
	ErrInvalidTime        = errors.New("transition time is zero or precedes the current revision")
	ErrInvalidTransition  = errors.New("invalid run state transition")
	ErrUninitialized      = errors.New("run has not been created")
	ErrOutcomeUnconfirmed = errors.New("external outcome requires reconciliation")
	ErrVersionExhausted   = errors.New("run revision exhausted")
)

// Run is the execution aggregate. It never carries credentials, database handles or money.
// Its lifecycle says nothing about financial settlement; that belongs to commerce.
type Run struct {
	id          RunID
	workspaceID WorkspaceID
	state       State
	version     uint64
	createdAt   time.Time
	updatedAt   time.Time
}

// Snapshot is a detached read value, not a mutation or rehydration API.
type Snapshot struct {
	ID          RunID
	WorkspaceID WorkspaceID
	State       State
	Version     uint64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Restore accepts trusted storage snapshots only. It validates persisted invariants;
// no inbound DTO may use it as a state-changing command.
func Restore(s Snapshot) (Run, error) {
	if !s.ID.IsValid() || !s.WorkspaceID.IsValid() {
		return Run{}, ErrInvalidIdentity
	}
	if s.Version == 0 || s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() || s.UpdatedAt.Before(s.CreatedAt) || s.CreatedAt.Year() < 1 || s.UpdatedAt.Year() > 9999 {
		return Run{}, ErrInvalidTime
	}
	switch s.State {
	case Queued, Running, WaitingInput, CancelRequested, Reconciling, Succeeded, Failed, Canceled, TimedOut:
	default:
		return Run{}, ErrInvalidTransition
	}
	if (s.State == Queued) != (s.Version == 1) {
		return Run{}, ErrInvalidTransition
	}
	return Run{id: s.ID, workspaceID: s.WorkspaceID, state: s.State, version: s.Version, createdAt: s.CreatedAt.UTC(), updatedAt: s.UpdatedAt.UTC()}, nil
}

// NewQueuedRun must eventually be used inside atomic admission, not as an execution shortcut.
// No creation endpoint or admission transaction is implemented by this package.
func NewQueuedRun(id RunID, workspaceID WorkspaceID, at time.Time) (Run, error) {
	if !id.IsValid() || !workspaceID.IsValid() {
		return Run{}, ErrInvalidIdentity
	}
	if at.IsZero() {
		return Run{}, ErrInvalidTime
	}
	return Run{id: id, workspaceID: workspaceID, state: Queued, version: 1, createdAt: at.UTC(), updatedAt: at.UTC()}, nil
}

func (r Run) Snapshot() Snapshot {
	return Snapshot{ID: r.id, WorkspaceID: r.workspaceID, State: r.state, Version: r.version, CreatedAt: r.createdAt, UpdatedAt: r.updatedAt}
}

func (r Run) checkTime(at time.Time) error {
	if r.version == 0 {
		return ErrUninitialized
	}
	if at.IsZero() || at.Before(r.updatedAt) {
		return ErrInvalidTime
	}
	return nil
}

func (r *Run) move(next State, at time.Time, allowed ...State) error {
	if err := r.checkTime(at); err != nil {
		return err
	}
	for _, state := range allowed {
		if r.state != state {
			continue
		}
		if r.version == ^uint64(0) {
			return ErrVersionExhausted
		}
		r.state, r.version, r.updatedAt = next, r.version+1, at.UTC()
		return nil
	}
	return ErrInvalidTransition
}

func (r *Run) Start(at time.Time) error        { return r.move(Running, at, Queued) }
func (r *Run) WaitForInput(at time.Time) error { return r.move(WaitingInput, at, Running) }
func (r *Run) Resume(at time.Time) error       { return r.move(Running, at, WaitingInput) }

// RequestCancel is idempotent. Only a queued run can be safely canceled locally.
// Once submitted, a worker must request upstream cancellation and later confirm the outcome.
func (r *Run) RequestCancel(at time.Time) (bool, error) {
	if err := r.checkTime(at); err != nil {
		return false, err
	}
	if r.state.IsTerminal() || r.state == CancelRequested {
		return false, nil
	}
	if r.state == Reconciling {
		return false, ErrOutcomeUnconfirmed
	}
	if r.state == Queued {
		err := r.move(Canceled, at, Queued)
		return err == nil, err
	}
	err := r.move(CancelRequested, at, Running, WaitingInput)
	return err == nil, err
}

func (r *Run) MarkOutcomeUnconfirmed(at time.Time) error {
	if r.state == Reconciling {
		return r.checkTime(at)
	}
	return r.move(Reconciling, at, Running, WaitingInput, CancelRequested)
}

func (r *Run) confirm(next State, at time.Time) error {
	if r.state == next {
		return r.checkTime(at)
	}
	return r.move(next, at, Running, WaitingInput, CancelRequested, Reconciling)
}

// Confirm methods require trusted execution evidence; they are not caller-selected statuses.
func (r *Run) ConfirmSucceeded(at time.Time) error { return r.confirm(Succeeded, at) }
func (r *Run) ConfirmFailed(at time.Time) error    { return r.confirm(Failed, at) }
func (r *Run) ConfirmCanceled(at time.Time) error {
	if r.state == Canceled {
		return r.checkTime(at)
	}
	return r.move(Canceled, at, CancelRequested, Reconciling)
}

// TimeoutBeforeStart never claims that a remote operation stopped on a network timeout.
func (r *Run) TimeoutBeforeStart(at time.Time) error { return r.move(TimedOut, at, Queued) }
