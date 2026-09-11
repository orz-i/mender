package domain

import (
	"errors"
	"time"
	"unicode/utf8"
)

type JobState string

const (
	JobBlocked         JobState = "blocked"
	JobQueued          JobState = "queued"
	JobLeased          JobState = "leased"
	JobProviderWaiting JobState = "provider_waiting"
	JobReconciling     JobState = "reconciling"
	JobFinished        JobState = "finished"
	JobCanceled        JobState = "canceled"
)

const (
	BlockExecutorNotConfigured    = "executor_not_configured"
	BlockAttemptLimitReached      = "attempt_limit_reached"
	BlockSubmissionOutcomeUnknown = "submission_outcome_unknown"
)

var (
	ErrInvalidJob        = errors.New("invalid execution job")
	ErrJobState          = errors.New("invalid execution job transition")
	ErrLeaseLost         = errors.New("execution job lease lost")
	ErrLeaseExpired      = errors.New("execution job lease expired")
	ErrAttemptLimit      = errors.New("execution job attempt limit reached")
	ErrInvalidWorker     = errors.New("invalid worker identifier")
	ErrInvalidJobTime    = errors.New("invalid execution job time")
	ErrSubmissionState   = errors.New("invalid supplier submission transition")
	ErrInvalidSubmission = errors.New("invalid supplier submission facts")
)

type LeaseToken struct {
	WorkspaceID WorkspaceID
	RunID       RunID
	WorkerID    string
	Generation  uint64
}

// MarkProviderWaiting consumes the local execution lease after the supplier has
// durably acknowledged the submission. Provider processing is asynchronous and
// must not keep a Worker lease alive until remote completion.
func (j *Job) MarkProviderWaiting(token LeaseToken, at time.Time) error {
	if err := j.checkTime(at); err != nil {
		return err
	}
	if j.snapshot.State != JobLeased || !j.matches(token) {
		return ErrLeaseLost
	}
	if !at.Before(j.snapshot.LeaseUntil) {
		return ErrLeaseExpired
	}
	j.snapshot.State = JobProviderWaiting
	j.snapshot.LeaseOwner = ""
	j.snapshot.LeaseUntil = time.Time{}
	j.snapshot.UpdatedAt = at.UTC()
	return nil
}

// FinishFromProvider is only for a trusted provider-result transaction. It is
// deliberately unavailable from queued/blocked jobs so a local worker cannot
// invent a terminal outcome without remote evidence.
func (j *Job) FinishFromProvider(at time.Time) error {
	if err := j.checkTime(at); err != nil {
		return err
	}
	if j.snapshot.State != JobProviderWaiting && j.snapshot.State != JobReconciling {
		return ErrJobState
	}
	j.snapshot.State = JobFinished
	j.snapshot.BlockedReason = ""
	j.snapshot.LeaseOwner = ""
	j.snapshot.LeaseUntil = time.Time{}
	j.snapshot.StoppedAt = at.UTC()
	j.snapshot.UpdatedAt = at.UTC()
	return nil
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
	case JobProviderWaiting:
		if s.BlockedReason != "" || s.LeaseOwner != "" || !s.LeaseUntil.IsZero() || s.AttemptCount == 0 || s.LeaseGeneration == 0 || !s.StoppedAt.IsZero() {
			return Job{}, ErrInvalidJob
		}
	case JobReconciling:
		if s.BlockedReason != BlockSubmissionOutcomeUnknown || s.LeaseOwner != "" || !s.LeaseUntil.IsZero() || s.AttemptCount == 0 || s.LeaseGeneration == 0 || !s.StoppedAt.IsZero() {
			return Job{}, ErrInvalidJob
		}
	case JobFinished:
		if s.BlockedReason != "" || s.LeaseOwner != "" || !s.LeaseUntil.IsZero() || s.AttemptCount == 0 || s.LeaseGeneration == 0 || !validJobTime(s.StoppedAt) || s.StoppedAt.Before(s.CreatedAt) {
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

func (j *Job) moveToSubmissionReconciliation(at time.Time) {
	j.snapshot.State = JobReconciling
	j.snapshot.BlockedReason = BlockSubmissionOutcomeUnknown
	j.snapshot.LeaseOwner = ""
	j.snapshot.LeaseUntil = time.Time{}
	j.snapshot.AvailableAt = at.UTC()
	j.snapshot.UpdatedAt = at.UTC()
}

// MarkSubmissionUnknown is used by the current lease owner after an attempted
// supplier call has an indeterminate result. It consumes the lease locally and
// makes the Job non-leaseable until a reconciliation use case resolves it.
func (j *Job) MarkSubmissionUnknown(token LeaseToken, at time.Time) error {
	if err := j.checkTime(at); err != nil {
		return err
	}
	if j.snapshot.State != JobLeased || !j.matches(token) {
		return ErrLeaseLost
	}
	if !at.Before(j.snapshot.LeaseUntil) {
		return ErrLeaseExpired
	}
	j.moveToSubmissionReconciliation(at)
	return nil
}

// RecoverSubmissionUnknown is the crash-recovery counterpart. A submitting or
// submitted Attempt must never be requeued after its lease expires.
func (j *Job) RecoverSubmissionUnknown(at time.Time) error {
	if err := j.checkTime(at); err != nil {
		return err
	}
	if j.snapshot.State != JobLeased {
		return ErrJobState
	}
	if at.Before(j.snapshot.LeaseUntil) {
		return ErrLeaseExpired
	}
	j.moveToSubmissionReconciliation(at)
	return nil
}

type AttemptState string

const (
	AttemptLeased     AttemptState = "leased"
	AttemptReleased   AttemptState = "released"
	AttemptExpired    AttemptState = "expired"
	AttemptSubmitting AttemptState = "submitting"
	AttemptSubmitted  AttemptState = "submitted"
	AttemptUnknown    AttemptState = "unknown"
)

type AttemptSnapshot struct {
	WorkspaceID        WorkspaceID
	RunID              RunID
	AttemptNo          uint32
	LeaseGeneration    uint64
	LeaseOwner         string
	State              AttemptState
	LeasedAt           time.Time
	LeaseUntil         time.Time
	FinishedAt         time.Time
	SubmissionKey      string
	SubmissionIntentAt time.Time
	ProviderID         string
	ProviderRequestID  string
	ExternalTaskID     string
	SubmittedAt        time.Time
	UnknownAt          time.Time
	UnknownReason      string
}

type Attempt struct{ snapshot AttemptSnapshot }

func validSubmissionKey(v string) bool {
	if len(v) < 8 || len(v) > 200 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == ':') {
			return false
		}
	}
	return true
}

func validProviderValue(v string, required bool) bool {
	if v == "" {
		return !required
	}
	if len(v) > 512 || !utf8.ValidString(v) {
		return false
	}
	for _, c := range v {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

func validUnknownReason(v string) bool {
	return v != "" && len(v) <= 500 && utf8.ValidString(v)
}

func RestoreAttempt(a AttemptSnapshot) (Attempt, error) {
	if !a.WorkspaceID.IsValid() || !a.RunID.IsValid() || a.AttemptNo == 0 || uint64(a.AttemptNo) != a.LeaseGeneration || !validID(a.LeaseOwner) || !validJobTime(a.LeasedAt) || !validJobTime(a.LeaseUntil) || !a.LeaseUntil.After(a.LeasedAt) {
		return Attempt{}, ErrInvalidJob
	}
	switch a.State {
	case AttemptLeased:
		if !a.FinishedAt.IsZero() || a.SubmissionKey != "" || !a.SubmissionIntentAt.IsZero() || a.ProviderID != "" || a.ProviderRequestID != "" || a.ExternalTaskID != "" || !a.SubmittedAt.IsZero() || !a.UnknownAt.IsZero() || a.UnknownReason != "" {
			return Attempt{}, ErrInvalidJob
		}
	case AttemptReleased, AttemptExpired:
		if !validJobTime(a.FinishedAt) || a.FinishedAt.Before(a.LeasedAt) || a.SubmissionKey != "" || !a.SubmissionIntentAt.IsZero() || a.ProviderID != "" || a.ProviderRequestID != "" || a.ExternalTaskID != "" || !a.SubmittedAt.IsZero() || !a.UnknownAt.IsZero() || a.UnknownReason != "" {
			return Attempt{}, ErrInvalidJob
		}
	case AttemptSubmitting:
		if !validSubmissionKey(a.SubmissionKey) || !validJobTime(a.SubmissionIntentAt) || a.SubmissionIntentAt.Before(a.LeasedAt) || !a.SubmissionIntentAt.Before(a.LeaseUntil) || a.ProviderID != "" || a.ProviderRequestID != "" || a.ExternalTaskID != "" || !a.SubmittedAt.IsZero() || !a.UnknownAt.IsZero() || a.UnknownReason != "" || !a.FinishedAt.IsZero() {
			return Attempt{}, ErrInvalidJob
		}
	case AttemptSubmitted:
		if !validSubmissionKey(a.SubmissionKey) || !validJobTime(a.SubmissionIntentAt) || a.SubmissionIntentAt.Before(a.LeasedAt) || !a.SubmissionIntentAt.Before(a.LeaseUntil) || (a.ProviderID != "" && !validID(a.ProviderID)) || !validProviderValue(a.ProviderRequestID, true) || !validProviderValue(a.ExternalTaskID, false) || !validJobTime(a.SubmittedAt) || a.SubmittedAt.Before(a.SubmissionIntentAt) || !a.SubmittedAt.Before(a.LeaseUntil) || !a.UnknownAt.IsZero() || a.UnknownReason != "" || !a.FinishedAt.IsZero() {
			return Attempt{}, ErrInvalidJob
		}
	case AttemptUnknown:
		knownProvider := a.ProviderRequestID != ""
		if !validSubmissionKey(a.SubmissionKey) || !validJobTime(a.SubmissionIntentAt) || a.SubmissionIntentAt.Before(a.LeasedAt) || !a.SubmissionIntentAt.Before(a.LeaseUntil) || (a.ProviderID != "" && !validID(a.ProviderID)) || (!knownProvider && (a.ProviderID != "" || a.ExternalTaskID != "")) || !validProviderValue(a.ProviderRequestID, false) || !validProviderValue(a.ExternalTaskID, false) || (!a.SubmittedAt.IsZero() && (!validJobTime(a.SubmittedAt) || a.SubmittedAt.Before(a.SubmissionIntentAt) || !a.SubmittedAt.Before(a.LeaseUntil))) || !validJobTime(a.UnknownAt) || a.UnknownAt.Before(a.SubmissionIntentAt) || !validUnknownReason(a.UnknownReason) || !a.FinishedAt.Equal(a.UnknownAt) {
			return Attempt{}, ErrInvalidJob
		}
	default:
		return Attempt{}, ErrInvalidJob
	}
	a.LeasedAt = a.LeasedAt.UTC()
	a.LeaseUntil = a.LeaseUntil.UTC()
	if !a.FinishedAt.IsZero() {
		a.FinishedAt = a.FinishedAt.UTC()
	}
	if !a.SubmissionIntentAt.IsZero() {
		a.SubmissionIntentAt = a.SubmissionIntentAt.UTC()
	}
	if !a.SubmittedAt.IsZero() {
		a.SubmittedAt = a.SubmittedAt.UTC()
	}
	if !a.UnknownAt.IsZero() {
		a.UnknownAt = a.UnknownAt.UTC()
	}
	return Attempt{snapshot: a}, nil
}

func ValidateAttempt(a AttemptSnapshot) error {
	_, err := RestoreAttempt(a)
	return err
}

func (a Attempt) Snapshot() AttemptSnapshot { return a.snapshot }

func (a *Attempt) matches(token LeaseToken) bool {
	return token.WorkspaceID == a.snapshot.WorkspaceID && token.RunID == a.snapshot.RunID && token.WorkerID == a.snapshot.LeaseOwner && token.Generation == a.snapshot.LeaseGeneration
}

func (a *Attempt) checkActive(token LeaseToken, at time.Time) error {
	if !a.matches(token) {
		return ErrLeaseLost
	}
	if !validJobTime(at) || at.Before(a.snapshot.LeasedAt) {
		return ErrInvalidJobTime
	}
	if !at.Before(a.snapshot.LeaseUntil) {
		return ErrLeaseExpired
	}
	return nil
}

func (a *Attempt) BeginSubmission(token LeaseToken, at time.Time, key string) error {
	if err := a.checkActive(token, at); err != nil {
		return err
	}
	if !validSubmissionKey(key) {
		return ErrInvalidSubmission
	}
	if a.snapshot.State == AttemptSubmitting && a.snapshot.SubmissionKey == key {
		return nil
	}
	if a.snapshot.State != AttemptLeased {
		return ErrSubmissionState
	}
	a.snapshot.State = AttemptSubmitting
	a.snapshot.SubmissionKey = key
	a.snapshot.SubmissionIntentAt = at.UTC()
	return nil
}

func (a *Attempt) MarkSubmitted(token LeaseToken, at time.Time, key, providerID, providerRequestID, externalTaskID string) error {
	if err := a.checkActive(token, at); err != nil {
		return err
	}
	if !validSubmissionKey(key) || !validID(providerID) || !validProviderValue(providerRequestID, true) || !validProviderValue(externalTaskID, false) {
		return ErrInvalidSubmission
	}
	if a.snapshot.State == AttemptSubmitted && a.snapshot.SubmissionKey == key && a.snapshot.ProviderID == providerID && a.snapshot.ProviderRequestID == providerRequestID && a.snapshot.ExternalTaskID == externalTaskID {
		return nil
	}
	if a.snapshot.State != AttemptSubmitting || a.snapshot.SubmissionKey != key || at.Before(a.snapshot.SubmissionIntentAt) {
		return ErrSubmissionState
	}
	a.snapshot.State = AttemptSubmitted
	a.snapshot.ProviderID = providerID
	a.snapshot.ProviderRequestID = providerRequestID
	a.snapshot.ExternalTaskID = externalTaskID
	a.snapshot.SubmittedAt = at.UTC()
	return nil
}

func (a *Attempt) MarkUnknown(token LeaseToken, at time.Time, key, providerID, providerRequestID, externalTaskID, reason string) error {
	if err := a.checkActive(token, at); err != nil {
		return err
	}
	knownProvider := providerRequestID != ""
	if !validSubmissionKey(key) || !validUnknownReason(reason) || (knownProvider && !validID(providerID)) || (!knownProvider && (providerID != "" || externalTaskID != "")) || !validProviderValue(providerRequestID, false) || !validProviderValue(externalTaskID, false) {
		return ErrInvalidSubmission
	}
	if (a.snapshot.State != AttemptSubmitting && a.snapshot.State != AttemptSubmitted) || a.snapshot.SubmissionKey != key || at.Before(a.snapshot.SubmissionIntentAt) {
		return ErrSubmissionState
	}
	if a.snapshot.State == AttemptSubmitted {
		if knownProvider && (a.snapshot.ProviderID != providerID || a.snapshot.ProviderRequestID != providerRequestID || a.snapshot.ExternalTaskID != externalTaskID) {
			return ErrInvalidSubmission
		}
	} else if knownProvider {
		a.snapshot.ProviderID = providerID
		a.snapshot.ProviderRequestID = providerRequestID
		a.snapshot.ExternalTaskID = externalTaskID
	}
	a.snapshot.State = AttemptUnknown
	a.snapshot.UnknownAt = at.UTC()
	a.snapshot.UnknownReason = reason
	a.snapshot.FinishedAt = at.UTC()
	return nil
}

func (a *Attempt) RecoverUnknown(at time.Time, reason string) error {
	if !validJobTime(at) || at.Before(a.snapshot.LeaseUntil) || !validUnknownReason(reason) {
		return ErrInvalidSubmission
	}
	if a.snapshot.State != AttemptSubmitting && a.snapshot.State != AttemptSubmitted {
		return ErrSubmissionState
	}
	a.snapshot.State = AttemptUnknown
	a.snapshot.UnknownAt = at.UTC()
	a.snapshot.UnknownReason = reason
	a.snapshot.FinishedAt = at.UTC()
	return nil
}
