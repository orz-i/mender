package domain

import (
	"errors"
	"time"
)

type JobState string

const (
	JobBlocked  JobState = "blocked"
	JobQueued   JobState = "queued"
	JobLeased   JobState = "leased"
	JobCanceled JobState = "canceled"
)

const (
	BlockExecutorNotConfigured = "executor_not_configured"
	BlockAttemptLimitReached   = "attempt_limit_reached"
)

var (
	ErrInvalidJob     = errors.New("invalid execution job")
	ErrJobState       = errors.New("invalid execution job transition")
	ErrLeaseLost      = errors.New("execution job lease lost")
	ErrLeaseExpired   = errors.New("execution job lease expired")
	ErrAttemptLimit   = errors.New("execution job attempt limit reached")
	ErrInvalidWorker  = errors.New("invalid worker identifier")
	ErrInvalidJobTime = errors.New("invalid execution job time")
)

type LeaseToken struct {
	WorkspaceID WorkspaceID
	RunID       RunID
	WorkerID    string
	Generation  uint64
}

type JobSnapshot struct {
	WorkspaceID     WorkspaceID
	RunID           RunID
	State           JobState
	BlockedReason   string
	AvailableAt     time.Time
	Priority        int
	LeaseOwner      string
	LeaseUntil      time.Time
	LeaseGeneration uint64
	AttemptCount    uint32
	MaxAttempts     uint32
	CreatedAt       time.Time
	UpdatedAt       time.Time
	StoppedAt       time.Time
}

type Job struct{ snapshot JobSnapshot }

func validJobTime(at time.Time) bool { return !at.IsZero() && at.Year() >= 1 && at.Year() <= 9999 }

func RestoreJob(s JobSnapshot) (Job, error) {
	if !s.WorkspaceID.IsValid() || !s.RunID.IsValid() || !validJobTime(s.CreatedAt) || !validJobTime(s.UpdatedAt) || !validJobTime(s.AvailableAt) || s.UpdatedAt.Before(s.CreatedAt) || s.AvailableAt.Before(s.CreatedAt) || s.MaxAttempts == 0 || s.MaxAttempts > 100 || s.AttemptCount > s.MaxAttempts || s.LeaseGeneration != uint64(s.AttemptCount) {
		return Job{}, ErrInvalidJob
	}
	switch s.State {
	case JobBlocked:
		if s.BlockedReason != BlockExecutorNotConfigured && s.BlockedReason != BlockAttemptLimitReached || s.LeaseOwner != "" || !s.LeaseUntil.IsZero() || !s.StoppedAt.IsZero() {
			return Job{}, ErrInvalidJob
		}
	case JobQueued:
		if s.BlockedReason != "" || s.LeaseOwner != "" || !s.LeaseUntil.IsZero() || !s.StoppedAt.IsZero() {
			return Job{}, ErrInvalidJob
		}
	case JobLeased:
		if s.BlockedReason != "" || !validID(s.LeaseOwner) || !validJobTime(s.LeaseUntil) || !s.LeaseUntil.After(s.UpdatedAt) || s.LeaseGeneration == 0 || s.AttemptCount == 0 || !s.StoppedAt.IsZero() {
			return Job{}, ErrInvalidJob
		}
	case JobCanceled:
		if s.BlockedReason != "" && s.BlockedReason != BlockExecutorNotConfigured && s.BlockedReason != BlockAttemptLimitReached || s.LeaseOwner != "" || !s.LeaseUntil.IsZero() || !validJobTime(s.StoppedAt) || s.StoppedAt.Before(s.CreatedAt) {
			return Job{}, ErrInvalidJob
		}
	default:
		return Job{}, ErrInvalidJob
	}
	s.CreatedAt = s.CreatedAt.UTC()
	s.UpdatedAt = s.UpdatedAt.UTC()
	s.AvailableAt = s.AvailableAt.UTC()
	if !s.LeaseUntil.IsZero() {
		s.LeaseUntil = s.LeaseUntil.UTC()
	}
	if !s.StoppedAt.IsZero() {
		s.StoppedAt = s.StoppedAt.UTC()
	}
	return Job{snapshot: s}, nil
}

func NewBlockedJob(workspace WorkspaceID, run RunID, at time.Time, maxAttempts uint32) (Job, error) {
	return RestoreJob(JobSnapshot{WorkspaceID: workspace, RunID: run, State: JobBlocked, BlockedReason: BlockExecutorNotConfigured, AvailableAt: at, MaxAttempts: maxAttempts, CreatedAt: at, UpdatedAt: at})
}

func (j Job) Snapshot() JobSnapshot { return j.snapshot }

func (j *Job) checkTime(at time.Time) error {
	if !validJobTime(at) || at.Before(j.snapshot.UpdatedAt) {
		return ErrInvalidJobTime
	}
	return nil
}

func (j *Job) Activate(at time.Time) error {
	if err := j.checkTime(at); err != nil {
		return err
	}
	if j.snapshot.State != JobBlocked || j.snapshot.BlockedReason != BlockExecutorNotConfigured {
		return ErrJobState
	}
	j.snapshot.State = JobQueued
	j.snapshot.BlockedReason = ""
	if j.snapshot.AvailableAt.Before(at) {
		j.snapshot.AvailableAt = at.UTC()
	}
	j.snapshot.UpdatedAt = at.UTC()
	return nil
}

// CancelNeverLeased is the only local Job cancellation eligible for immediate
// reservation release. A non-zero lease generation means a Worker has owned an
// attempt, so the caller must use a later submission-aware cancellation path.
func (j *Job) CancelNeverLeased(at time.Time) error {
	if err := j.checkTime(at); err != nil {
		return err
	}
	if j.snapshot.AttemptCount != 0 || j.snapshot.LeaseGeneration != 0 || j.snapshot.LeaseOwner != "" || !j.snapshot.LeaseUntil.IsZero() {
		return ErrJobState
	}
	switch j.snapshot.State {
	case JobBlocked:
		if j.snapshot.BlockedReason != BlockExecutorNotConfigured {
			return ErrJobState
		}
	case JobQueued:
		if j.snapshot.BlockedReason != "" {
			return ErrJobState
		}
	default:
		return ErrJobState
	}
	j.snapshot.State = JobCanceled
	j.snapshot.StoppedAt = at.UTC()
	j.snapshot.UpdatedAt = at.UTC()
	return nil
}

func (j *Job) Acquire(worker string, at, until time.Time) (LeaseToken, error) {
	if err := j.checkTime(at); err != nil {
		return LeaseToken{}, err
	}
	if !validID(worker) {
		return LeaseToken{}, ErrInvalidWorker
	}
	if j.snapshot.State != JobQueued || at.Before(j.snapshot.AvailableAt) {
		return LeaseToken{}, ErrJobState
	}
	if j.snapshot.AttemptCount >= j.snapshot.MaxAttempts {
		return LeaseToken{}, ErrAttemptLimit
	}
	if !validJobTime(until) || !until.After(at) {
		return LeaseToken{}, ErrInvalidJobTime
	}
	if j.snapshot.LeaseGeneration == ^uint64(0) {
		return LeaseToken{}, ErrAttemptLimit
	}
	j.snapshot.State = JobLeased
	j.snapshot.LeaseOwner = worker
	j.snapshot.LeaseUntil = until.UTC()
	j.snapshot.LeaseGeneration++
	j.snapshot.AttemptCount++
	j.snapshot.UpdatedAt = at.UTC()
	return LeaseToken{WorkspaceID: j.snapshot.WorkspaceID, RunID: j.snapshot.RunID, WorkerID: worker, Generation: j.snapshot.LeaseGeneration}, nil
}

func (j *Job) matches(token LeaseToken) bool {
	return token.WorkspaceID == j.snapshot.WorkspaceID && token.RunID == j.snapshot.RunID && token.WorkerID == j.snapshot.LeaseOwner && token.Generation == j.snapshot.LeaseGeneration
}

func (j *Job) Renew(token LeaseToken, at, until time.Time) error {
	if err := j.checkTime(at); err != nil {
		return err
	}
	if j.snapshot.State != JobLeased || !j.matches(token) {
		return ErrLeaseLost
	}
	if !at.Before(j.snapshot.LeaseUntil) {
		return ErrLeaseExpired
	}
	if !validJobTime(until) || !until.After(j.snapshot.LeaseUntil) {
		return ErrInvalidJobTime
	}
	j.snapshot.LeaseUntil = until.UTC()
	j.snapshot.UpdatedAt = at.UTC()
	return nil
}

func (j *Job) ReleaseBeforeSubmit(token LeaseToken, at, availableAt time.Time) error {
	if err := j.checkTime(at); err != nil {
		return err
	}
	if j.snapshot.State != JobLeased || !j.matches(token) {
		return ErrLeaseLost
	}
	if !at.Before(j.snapshot.LeaseUntil) {
		return ErrLeaseExpired
	}
	if !validJobTime(availableAt) || availableAt.Before(at) {
		return ErrInvalidJobTime
	}
	j.snapshot.State = JobQueued
	j.snapshot.LeaseOwner = ""
	j.snapshot.LeaseUntil = time.Time{}
	j.snapshot.AvailableAt = availableAt.UTC()
	j.snapshot.UpdatedAt = at.UTC()
	return nil
}

func (j *Job) RecoverExpired(at time.Time) error {
	if err := j.checkTime(at); err != nil {
		return err
	}
	if j.snapshot.State != JobLeased {
		return ErrJobState
	}
	if at.Before(j.snapshot.LeaseUntil) {
		return ErrLeaseExpired
	}
	j.snapshot.LeaseOwner = ""
	j.snapshot.LeaseUntil = time.Time{}
	j.snapshot.AvailableAt = at.UTC()
	j.snapshot.UpdatedAt = at.UTC()
	if j.snapshot.AttemptCount >= j.snapshot.MaxAttempts {
		j.snapshot.State = JobBlocked
		j.snapshot.BlockedReason = BlockAttemptLimitReached
	} else {
		j.snapshot.State = JobQueued
		j.snapshot.BlockedReason = ""
	}
	return nil
}

type AttemptState string

const (
	AttemptLeased   AttemptState = "leased"
	AttemptReleased AttemptState = "released"
	AttemptExpired  AttemptState = "expired"
)

type AttemptSnapshot struct {
	WorkspaceID     WorkspaceID
	RunID           RunID
	AttemptNo       uint32
	LeaseGeneration uint64
	LeaseOwner      string
	State           AttemptState
	LeasedAt        time.Time
	LeaseUntil      time.Time
	FinishedAt      time.Time
}

func ValidateAttempt(a AttemptSnapshot) error {
	if !a.WorkspaceID.IsValid() || !a.RunID.IsValid() || a.AttemptNo == 0 || uint64(a.AttemptNo) != a.LeaseGeneration || !validID(a.LeaseOwner) || !validJobTime(a.LeasedAt) || !validJobTime(a.LeaseUntil) || !a.LeaseUntil.After(a.LeasedAt) {
		return ErrInvalidJob
	}
	switch a.State {
	case AttemptLeased:
		if !a.FinishedAt.IsZero() {
			return ErrInvalidJob
		}
	case AttemptReleased, AttemptExpired:
		if !validJobTime(a.FinishedAt) || a.FinishedAt.Before(a.LeasedAt) {
			return ErrInvalidJob
		}
	default:
		return ErrInvalidJob
	}
	return nil
}
