package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type Workers struct{ pool *pgxpool.Pool }

func NewWorkers(pool *pgxpool.Pool) *Workers { return &Workers{pool: pool} }

func (w *Workers) scoped(ctx context.Context, workspace domain.WorkspaceID) (pgx.Tx, error) {
	if w == nil || w.pool == nil || !workspace.IsValid() {
		return nil, application.ErrWorkerUnavailable
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrWorkerUnavailable
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, string(workspace)); err != nil {
		rollback(tx)
		return nil, application.ErrWorkerUnavailable
	}
	return tx, nil
}

func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func updateAttempt(ctx context.Context, tx pgx.Tx, before, after domain.AttemptSnapshot) error {
	tag, err := tx.Exec(ctx, `UPDATE execution.run_attempts SET state=$1,lease_until=$2,finished_at=$3,submission_key=$4,submission_intent_at=$5,provider_request_id=$6,external_task_id=$7,submitted_at=$8,unknown_at=$9,unknown_reason=$10
	 WHERE workspace_id=$11 AND run_id=$12 AND lease_generation=$13 AND lease_owner=$14 AND state=$15 AND lease_until=$16`, string(after.State), after.LeaseUntil, nullableTime(after.FinishedAt), nullableString(after.SubmissionKey), nullableTime(after.SubmissionIntentAt), nullableString(after.ProviderRequestID), nullableString(after.ExternalTaskID), nullableTime(after.SubmittedAt), nullableTime(after.UnknownAt), nullableString(after.UnknownReason), string(after.WorkspaceID), string(after.RunID), int64(after.LeaseGeneration), after.LeaseOwner, string(before.State), before.LeaseUntil)
	if err != nil {
		return application.ErrWorkerUnavailable
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrLeaseLost
	}
	return nil
}

func (w *Workers) loadAttemptForToken(ctx context.Context, tx pgx.Tx, token domain.LeaseToken) (domain.AttemptSnapshot, error) {
	attempt, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptColumns+` FROM execution.run_attempts a WHERE a.workspace_id=$1 AND a.run_id=$2 AND a.lease_generation=$3 FOR UPDATE`, string(token.WorkspaceID), string(token.RunID), int64(token.Generation)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AttemptSnapshot{}, domain.ErrLeaseLost
	}
	if err != nil || attempt.LeaseOwner != token.WorkerID {
		return domain.AttemptSnapshot{}, domain.ErrLeaseLost
	}
	return attempt, nil
}

func loadRunForUpdate(ctx context.Context, tx pgx.Tx, workspace domain.WorkspaceID, run domain.RunID) (domain.Run, error) {
	r, err := scanRun(tx.QueryRow(ctx, `SELECT workspace_id,id,state,version,created_at,updated_at FROM execution.runs WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, string(workspace), string(run)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Run{}, application.ErrWorkerUnavailable
	}
	return r, err
}

func saveWorkerRun(ctx context.Context, tx pgx.Tx, before domain.Snapshot, run domain.Run, token domain.LeaseToken, reason string) error {
	after := run.Snapshot()
	if after.Version != before.Version+1 || after.WorkspaceID != before.WorkspaceID || after.ID != before.ID {
		return application.ErrWorkerUnavailable
	}
	tag, err := tx.Exec(ctx, `UPDATE execution.runs SET state=$1,version=$2,updated_at=$3 WHERE workspace_id=$4 AND id=$5 AND state=$6 AND version=$7 AND updated_at=$8`, string(after.State), int64(after.Version), after.UpdatedAt, string(after.WorkspaceID), string(after.ID), string(before.State), int64(before.Version), before.UpdatedAt)
	if err != nil || tag.RowsAffected() != 1 {
		return domain.ErrLeaseLost
	}
	_, err = tx.Exec(ctx, `INSERT INTO execution.run_events(workspace_id,run_id,version,state,subject_id,credential_id,occurred_at,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, string(after.WorkspaceID), string(after.ID), int64(after.Version), string(after.State), token.WorkerID, fmt.Sprintf("lease_%d", token.Generation), after.UpdatedAt, reason)
	if err != nil {
		return application.ErrWorkerUnavailable
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanRun(row rowScanner) (domain.Run, error) {
	var s domain.Snapshot
	var workspace, run, state string
	var version int64
	if err := row.Scan(&workspace, &run, &state, &version, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return domain.Run{}, err
	}
	if version < 1 {
		return domain.Run{}, application.ErrWorkerUnavailable
	}
	s.WorkspaceID = domain.WorkspaceID(workspace)
	s.ID = domain.RunID(run)
	s.State = domain.State(state)
	s.Version = uint64(version)
	result, err := domain.Restore(s)
	if err != nil {
		return domain.Run{}, application.ErrWorkerUnavailable
	}
	return result, nil
}

func scanJob(row rowScanner) (domain.JobSnapshot, error) {
	var s domain.JobSnapshot
	var workspace, run, state string
	var blocked *string
	var leaseOwner *string
	var leaseUntil, stoppedAt *time.Time
	var generation int64
	var attempts, maxAttempts int32
	err := row.Scan(&workspace, &run, &state, &blocked, &s.AvailableAt, &s.Priority, &leaseOwner, &leaseUntil, &generation, &attempts, &maxAttempts, &s.CreatedAt, &s.UpdatedAt, &stoppedAt)
	if err != nil {
		return s, err
	}
	if generation < 0 || attempts < 0 || maxAttempts < 0 {
		return s, domain.ErrInvalidJob
	}
	s.WorkspaceID = domain.WorkspaceID(workspace)
	s.RunID = domain.RunID(run)
	s.State = domain.JobState(state)
	if blocked != nil {
		s.BlockedReason = *blocked
	}
	if leaseOwner != nil {
		s.LeaseOwner = *leaseOwner
	}
	if leaseUntil != nil {
		s.LeaseUntil = *leaseUntil
	}
	if stoppedAt != nil {
		s.StoppedAt = *stoppedAt
	}
	s.LeaseGeneration = uint64(generation)
	s.AttemptCount = uint32(attempts)
	s.MaxAttempts = uint32(maxAttempts)
	if _, err = domain.RestoreJob(s); err != nil {
		return domain.JobSnapshot{}, err
	}
	return s, nil
}

const jobColumns = `j.workspace_id,j.run_id,j.state,j.blocked_reason,j.available_at,j.priority,j.lease_owner,j.lease_until,j.lease_generation,j.attempt_count,j.max_attempts,j.created_at,j.updated_at,j.stopped_at`
const attemptColumns = `a.workspace_id,a.run_id,a.attempt_no,a.lease_generation,a.lease_owner,a.state,a.leased_at,a.lease_until,a.finished_at,a.submission_key,a.submission_intent_at,a.provider_request_id,a.external_task_id,a.submitted_at,a.unknown_at,a.unknown_reason`

func scanAttempt(row rowScanner) (domain.AttemptSnapshot, error) {
	var s domain.AttemptSnapshot
	var workspace, run, owner, state string
	var attemptNo int32
	var generation int64
	var finishedAt, intentAt, submittedAt, unknownAt *time.Time
	var submissionKey, providerRequestID, externalTaskID, unknownReason *string
	if err := row.Scan(&workspace, &run, &attemptNo, &generation, &owner, &state, &s.LeasedAt, &s.LeaseUntil, &finishedAt, &submissionKey, &intentAt, &providerRequestID, &externalTaskID, &submittedAt, &unknownAt, &unknownReason); err != nil {
		return s, err
	}
	if attemptNo < 1 || generation < 1 {
		return s, domain.ErrInvalidJob
	}
	s.WorkspaceID, s.RunID = domain.WorkspaceID(workspace), domain.RunID(run)
	s.AttemptNo, s.LeaseGeneration = uint32(attemptNo), uint64(generation)
	s.LeaseOwner, s.State = owner, domain.AttemptState(state)
	if finishedAt != nil {
		s.FinishedAt = *finishedAt
	}
	if submissionKey != nil {
		s.SubmissionKey = *submissionKey
	}
	if intentAt != nil {
		s.SubmissionIntentAt = *intentAt
	}
	if providerRequestID != nil {
		s.ProviderRequestID = *providerRequestID
	}
	if externalTaskID != nil {
		s.ExternalTaskID = *externalTaskID
	}
	if submittedAt != nil {
		s.SubmittedAt = *submittedAt
	}
	if unknownAt != nil {
		s.UnknownAt = *unknownAt
	}
	if unknownReason != nil {
		s.UnknownReason = *unknownReason
	}
	if err := domain.ValidateAttempt(s); err != nil {
		return domain.AttemptSnapshot{}, err
	}
	return s, nil
}

func updateJob(ctx context.Context, tx pgx.Tx, before, after domain.JobSnapshot) error {
	tag, err := tx.Exec(ctx, `UPDATE execution.jobs SET state=$1,blocked_reason=NULLIF($2,''),available_at=$3,lease_owner=NULLIF($4,''),lease_until=$5,lease_generation=$6,attempt_count=$7,updated_at=$8
 WHERE workspace_id=$9 AND run_id=$10 AND state=$11 AND lease_generation=$12 AND updated_at=$13`, string(after.State), after.BlockedReason, after.AvailableAt, after.LeaseOwner, nullableTime(after.LeaseUntil), int64(after.LeaseGeneration), int32(after.AttemptCount), after.UpdatedAt, string(after.WorkspaceID), string(after.RunID), string(before.State), int64(before.LeaseGeneration), before.UpdatedAt)
	if err != nil {
		return application.ErrWorkerUnavailable
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrLeaseLost
	}
	return nil
}

func nullableTime(at time.Time) any {
	if at.IsZero() {
		return nil
	}
	return at
}

func (w *Workers) ActivateOne(ctx context.Context, workspace domain.WorkspaceID, revisions []string, at time.Time) (bool, error) {
	tx, err := w.scoped(ctx, workspace)
	if err != nil {
		return false, err
	}
	defer rollback(tx)
	row := tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM execution.jobs j JOIN execution.run_admissions a ON (a.workspace_id,a.run_id)=(j.workspace_id,j.run_id)
 WHERE j.workspace_id=$1 AND j.state='blocked' AND j.blocked_reason='executor_not_configured' AND a.deployment_revision=ANY($2::text[])
 ORDER BY j.priority DESC,j.created_at,j.run_id FOR UPDATE OF j SKIP LOCKED LIMIT 1`, string(workspace), revisions)
	before, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, application.ErrWorkerUnavailable
	}
	job, _ := domain.RestoreJob(before)
	if err = job.Activate(at); err != nil {
		return false, err
	}
	after := job.Snapshot()
	if err = updateJob(ctx, tx, before, after); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, application.ErrWorkerUnavailable
	}
	return true, nil
}

func (w *Workers) Acquire(ctx context.Context, workspace domain.WorkspaceID, worker string, at, until time.Time) (domain.JobSnapshot, error) {
	tx, err := w.scoped(ctx, workspace)
	if err != nil {
		return domain.JobSnapshot{}, err
	}
	defer rollback(tx)
	row := tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM execution.jobs j WHERE j.workspace_id=$1 AND j.state='queued' AND j.available_at<=$2 AND j.attempt_count<j.max_attempts
 ORDER BY j.priority DESC,j.available_at,j.created_at,j.run_id FOR UPDATE SKIP LOCKED LIMIT 1`, string(workspace), at)
	before, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JobSnapshot{}, application.ErrNoWork
	}
	if err != nil {
		return domain.JobSnapshot{}, application.ErrWorkerUnavailable
	}
	job, _ := domain.RestoreJob(before)
	token, err := job.Acquire(worker, at, until)
	if err != nil {
		return domain.JobSnapshot{}, err
	}
	after := job.Snapshot()
	if err = updateJob(ctx, tx, before, after); err != nil {
		return domain.JobSnapshot{}, err
	}
	attempt := domain.AttemptSnapshot{WorkspaceID: workspace, RunID: after.RunID, AttemptNo: after.AttemptCount, LeaseGeneration: token.Generation, LeaseOwner: worker, State: domain.AttemptLeased, LeasedAt: at, LeaseUntil: until}
	if err = domain.ValidateAttempt(attempt); err != nil {
		return domain.JobSnapshot{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO execution.run_attempts(workspace_id,run_id,attempt_no,lease_generation,lease_owner,state,leased_at,lease_until) VALUES($1,$2,$3,$4,$5,'leased',$6,$7)`, string(workspace), string(after.RunID), int32(attempt.AttemptNo), int64(attempt.LeaseGeneration), worker, attempt.LeasedAt, attempt.LeaseUntil); err != nil {
		return domain.JobSnapshot{}, application.ErrWorkerUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.JobSnapshot{}, application.ErrWorkerUnavailable
	}
	return after, nil
}

func (w *Workers) loadForToken(ctx context.Context, tx pgx.Tx, token domain.LeaseToken) (domain.JobSnapshot, error) {
	row := tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM execution.jobs j WHERE j.workspace_id=$1 AND j.run_id=$2 FOR UPDATE`, string(token.WorkspaceID), string(token.RunID))
	s, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, domain.ErrLeaseLost
	}
	if err != nil {
		return s, application.ErrWorkerUnavailable
	}
	if s.State != domain.JobLeased || s.LeaseOwner != token.WorkerID || s.LeaseGeneration != token.Generation {
		return s, domain.ErrLeaseLost
	}
	return s, nil
}

func (w *Workers) Renew(ctx context.Context, token domain.LeaseToken, at, until time.Time) (domain.JobSnapshot, error) {
	tx, err := w.scoped(ctx, token.WorkspaceID)
	if err != nil {
		return domain.JobSnapshot{}, err
	}
	defer rollback(tx)
	before, err := w.loadForToken(ctx, tx, token)
	if err != nil {
		return domain.JobSnapshot{}, err
	}
	job, _ := domain.RestoreJob(before)
	if err = job.Renew(token, at, until); err != nil {
		return domain.JobSnapshot{}, err
	}
	after := job.Snapshot()
	if err = updateJob(ctx, tx, before, after); err != nil {
		return domain.JobSnapshot{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE execution.run_attempts SET lease_until=$1 WHERE workspace_id=$2 AND run_id=$3 AND lease_generation=$4 AND lease_owner=$5 AND state IN ('leased','submitting','submitted') AND lease_until=$6`, after.LeaseUntil, string(token.WorkspaceID), string(token.RunID), int64(token.Generation), token.WorkerID, before.LeaseUntil)
	if err != nil || tag.RowsAffected() != 1 {
		return domain.JobSnapshot{}, domain.ErrLeaseLost
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.JobSnapshot{}, application.ErrWorkerUnavailable
	}
	return after, nil
}

func (w *Workers) ReleaseBeforeSubmit(ctx context.Context, token domain.LeaseToken, at, availableAt time.Time) (domain.JobSnapshot, error) {
	tx, err := w.scoped(ctx, token.WorkspaceID)
	if err != nil {
		return domain.JobSnapshot{}, err
	}
	defer rollback(tx)
	before, err := w.loadForToken(ctx, tx, token)
	if err != nil {
		return domain.JobSnapshot{}, err
	}
	job, _ := domain.RestoreJob(before)
	if err = job.ReleaseBeforeSubmit(token, at, availableAt); err != nil {
		return domain.JobSnapshot{}, err
	}
	after := job.Snapshot()
	if err = updateJob(ctx, tx, before, after); err != nil {
		return domain.JobSnapshot{}, err
	}
	tag, err := tx.Exec(ctx, `UPDATE execution.run_attempts SET state='released',finished_at=$1 WHERE workspace_id=$2 AND run_id=$3 AND lease_generation=$4 AND lease_owner=$5 AND state='leased'`, at, string(token.WorkspaceID), string(token.RunID), int64(token.Generation), token.WorkerID)
	if err != nil || tag.RowsAffected() != 1 {
		return domain.JobSnapshot{}, domain.ErrLeaseLost
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.JobSnapshot{}, application.ErrWorkerUnavailable
	}
	return after, nil
}

func (w *Workers) BeginSubmission(ctx context.Context, token domain.LeaseToken, key string, at time.Time) (domain.AttemptSnapshot, error) {
	tx, err := w.scoped(ctx, token.WorkspaceID)
	if err != nil {
		return domain.AttemptSnapshot{}, err
	}
	defer rollback(tx)
	if _, err = w.loadForToken(ctx, tx, token); err != nil {
		return domain.AttemptSnapshot{}, err
	}
	before, err := w.loadAttemptForToken(ctx, tx, token)
	if err != nil {
		return domain.AttemptSnapshot{}, err
	}
	attempt, _ := domain.RestoreAttempt(before)
	if err = attempt.BeginSubmission(token, at, key); err != nil {
		return domain.AttemptSnapshot{}, err
	}
	after := attempt.Snapshot()
	if before.State != after.State || before.SubmissionKey != after.SubmissionKey {
		if err = updateAttempt(ctx, tx, before, after); err != nil {
			return domain.AttemptSnapshot{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.AttemptSnapshot{}, application.ErrWorkerUnavailable
	}
	return after, nil
}

func loadJobForUpdate(ctx context.Context, tx pgx.Tx, workspace domain.WorkspaceID, run domain.RunID) (domain.JobSnapshot, error) {
	job, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM execution.jobs j WHERE j.workspace_id=$1 AND j.run_id=$2 FOR UPDATE`, string(workspace), string(run)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JobSnapshot{}, domain.ErrLeaseLost
	}
	if err != nil {
		return domain.JobSnapshot{}, application.ErrWorkerUnavailable
	}
	return job, nil
}

func jobMatchesToken(job domain.JobSnapshot, token domain.LeaseToken) bool {
	return job.State == domain.JobLeased && job.WorkspaceID == token.WorkspaceID && job.RunID == token.RunID && job.LeaseOwner == token.WorkerID && job.LeaseGeneration == token.Generation
}

func (w *Workers) RecordSubmitted(ctx context.Context, token domain.LeaseToken, key, providerRequestID, externalTaskID string, at time.Time) (domain.Snapshot, domain.AttemptSnapshot, error) {
	tx, err := w.scoped(ctx, token.WorkspaceID)
	if err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	defer rollback(tx)
	run, err := loadRunForUpdate(ctx, tx, token.WorkspaceID, token.RunID)
	if err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	beforeRun := run.Snapshot()
	beforeJob, err := loadJobForUpdate(ctx, tx, token.WorkspaceID, token.RunID)
	if err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	beforeAttempt, err := w.loadAttemptForToken(ctx, tx, token)
	if err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	if beforeAttempt.State == domain.AttemptSubmitted {
		if beforeAttempt.SubmissionKey != key || beforeAttempt.ProviderRequestID != providerRequestID || beforeAttempt.ExternalTaskID != externalTaskID || beforeRun.State != domain.Running || !beforeRun.UpdatedAt.Equal(beforeAttempt.SubmittedAt) || beforeJob.State != domain.JobProviderWaiting || beforeJob.LeaseGeneration != token.Generation {
			return domain.Snapshot{}, domain.AttemptSnapshot{}, domain.ErrLeaseLost
		}
		if err = tx.Commit(ctx); err != nil {
			return domain.Snapshot{}, domain.AttemptSnapshot{}, application.ErrWorkerUnavailable
		}
		return beforeRun, beforeAttempt, nil
	}
	if !jobMatchesToken(beforeJob, token) {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, domain.ErrLeaseLost
	}
	attempt, _ := domain.RestoreAttempt(beforeAttempt)
	if err = attempt.MarkSubmitted(token, at, key, providerRequestID, externalTaskID); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	job, _ := domain.RestoreJob(beforeJob)
	if err = job.MarkProviderWaiting(token, at); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	afterAttempt, afterJob := attempt.Snapshot(), job.Snapshot()
	if beforeRun.State != domain.Queued {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, application.ErrWorkerUnavailable
	}
	if err = run.Start(at); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	if err = updateAttempt(ctx, tx, beforeAttempt, afterAttempt); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	if err = updateJob(ctx, tx, beforeJob, afterJob); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	if err = saveWorkerRun(ctx, tx, beforeRun, run, token, "supplier submission accepted"); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, application.ErrWorkerUnavailable
	}
	return run.Snapshot(), afterAttempt, nil
}

func (w *Workers) RecordSubmissionUnknown(ctx context.Context, token domain.LeaseToken, key, reason string, at time.Time) (domain.Snapshot, domain.AttemptSnapshot, error) {
	tx, err := w.scoped(ctx, token.WorkspaceID)
	if err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	defer rollback(tx)
	run, err := loadRunForUpdate(ctx, tx, token.WorkspaceID, token.RunID)
	if err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	beforeRun := run.Snapshot()
	beforeJob, err := loadJobForUpdate(ctx, tx, token.WorkspaceID, token.RunID)
	if err != nil || !jobMatchesToken(beforeJob, token) {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, domain.ErrLeaseLost
	}
	beforeAttempt, err := w.loadAttemptForToken(ctx, tx, token)
	if err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	attempt, _ := domain.RestoreAttempt(beforeAttempt)
	if err = attempt.MarkUnknown(token, at, key, reason); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	job, _ := domain.RestoreJob(beforeJob)
	if err = job.MarkSubmissionUnknown(token, at); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	if err = run.MarkSubmissionUnconfirmed(at); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	afterAttempt, afterJob := attempt.Snapshot(), job.Snapshot()
	if err = updateAttempt(ctx, tx, beforeAttempt, afterAttempt); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	if err = updateJob(ctx, tx, beforeJob, afterJob); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	if err = saveWorkerRun(ctx, tx, beforeRun, run, token, "supplier submission outcome unknown"); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Snapshot{}, domain.AttemptSnapshot{}, application.ErrWorkerUnavailable
	}
	return run.Snapshot(), afterAttempt, nil
}

func (w *Workers) RecoverExpired(ctx context.Context, workspace domain.WorkspaceID, at time.Time) (domain.JobSnapshot, bool, error) {
	tx, err := w.scoped(ctx, workspace)
	if err != nil {
		return domain.JobSnapshot{}, false, err
	}
	defer rollback(tx)
	// Lock Run before Job for any recovery that may change Run state. This matches
	// coordinated cancellation and prevents a Run->Job / Job->Run deadlock cycle.
	run, err := scanRun(tx.QueryRow(ctx, `SELECT r.workspace_id,r.id,r.state,r.version,r.created_at,r.updated_at
 FROM execution.runs r JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(r.workspace_id,r.id)
 WHERE j.workspace_id=$1 AND j.state='leased' AND j.lease_until<=$2
 ORDER BY j.lease_until,j.run_id FOR UPDATE OF r SKIP LOCKED LIMIT 1`, string(workspace), at))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JobSnapshot{}, false, nil
	}
	if err != nil {
		return domain.JobSnapshot{}, false, application.ErrWorkerUnavailable
	}
	beforeRun := run.Snapshot()
	before, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM execution.jobs j
 WHERE j.workspace_id=$1 AND j.run_id=$2 AND j.state='leased' AND j.lease_until<=$3 FOR UPDATE`, string(workspace), string(beforeRun.ID), at))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JobSnapshot{}, false, nil
	}
	if err != nil {
		return domain.JobSnapshot{}, false, application.ErrWorkerUnavailable
	}
	token := domain.LeaseToken{WorkspaceID: workspace, RunID: before.RunID, WorkerID: before.LeaseOwner, Generation: before.LeaseGeneration}
	beforeAttempt, err := w.loadAttemptForToken(ctx, tx, token)
	if err != nil {
		return domain.JobSnapshot{}, false, err
	}
	job, _ := domain.RestoreJob(before)
	switch beforeAttempt.State {
	case domain.AttemptLeased:
		if err = job.RecoverExpired(at); err != nil {
			return domain.JobSnapshot{}, false, err
		}
		after := job.Snapshot()
		if err = updateJob(ctx, tx, before, after); err != nil {
			return domain.JobSnapshot{}, false, err
		}
		tag, updateErr := tx.Exec(ctx, `UPDATE execution.run_attempts SET state='expired',finished_at=$1 WHERE workspace_id=$2 AND run_id=$3 AND lease_generation=$4 AND lease_owner=$5 AND state='leased'`, at, string(workspace), string(before.RunID), int64(before.LeaseGeneration), before.LeaseOwner)
		if updateErr != nil || tag.RowsAffected() != 1 {
			return domain.JobSnapshot{}, false, application.ErrWorkerUnavailable
		}
		if err = tx.Commit(ctx); err != nil {
			return domain.JobSnapshot{}, false, application.ErrWorkerUnavailable
		}
		return after, true, nil
	case domain.AttemptSubmitting, domain.AttemptSubmitted:
		reason := "worker lease expired after supplier submission intent"
		if beforeAttempt.State == domain.AttemptSubmitted {
			reason = "worker lease expired after supplier acceptance"
		}
		attempt, _ := domain.RestoreAttempt(beforeAttempt)
		if err = attempt.RecoverUnknown(at, reason); err != nil {
			return domain.JobSnapshot{}, false, err
		}
		if err = job.RecoverSubmissionUnknown(at); err != nil {
			return domain.JobSnapshot{}, false, err
		}
		if err = run.MarkSubmissionUnconfirmed(at); err != nil {
			return domain.JobSnapshot{}, false, err
		}
		afterAttempt, afterJob := attempt.Snapshot(), job.Snapshot()
		if err = updateAttempt(ctx, tx, beforeAttempt, afterAttempt); err != nil {
			return domain.JobSnapshot{}, false, err
		}
		if err = updateJob(ctx, tx, before, afterJob); err != nil {
			return domain.JobSnapshot{}, false, err
		}
		if err = saveWorkerRun(ctx, tx, beforeRun, run, token, reason); err != nil {
			return domain.JobSnapshot{}, false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return domain.JobSnapshot{}, false, application.ErrWorkerUnavailable
		}
		return afterJob, true, nil
	default:
		return domain.JobSnapshot{}, false, application.ErrWorkerUnavailable
	}
}

var _ application.WorkerRepository = (*Workers)(nil)
