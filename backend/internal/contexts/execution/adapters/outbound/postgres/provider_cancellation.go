package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
	"github.com/orz-i/mender/backend/internal/contexts/execution/domain"
)

type ProviderCancelRequests struct{ pool *pgxpool.Pool }
type ProviderCancellations struct{ pool *pgxpool.Pool }

func NewProviderCancelRequests(pool *pgxpool.Pool) *ProviderCancelRequests {
	return &ProviderCancelRequests{pool: pool}
}
func NewProviderCancellations(pool *pgxpool.Pool) *ProviderCancellations {
	return &ProviderCancellations{pool: pool}
}

func scanProviderCancel(row rowScanner) (domain.ProviderCancelSnapshot, error) {
	var s domain.ProviderCancelSnapshot
	var workspace, run, state string
	var attempt int32
	var externalTask, observationID, unknownReason *string
	var sendingAt, resolvedAt *time.Time
	if err := row.Scan(&workspace, &run, &attempt, &s.CancelKey, &s.ProviderID, &s.ProviderRequestID, &externalTask, &s.RequestedBySubject, &s.RequestedByCredential, &s.Reason, &state, &s.RequestedAt, &sendingAt, &resolvedAt, &observationID, &unknownReason); err != nil {
		return domain.ProviderCancelSnapshot{}, err
	}
	s.WorkspaceID, s.RunID, s.AttemptNo, s.State = domain.WorkspaceID(workspace), domain.RunID(run), uint32(attempt), domain.ProviderCancelState(state)
	if externalTask != nil {
		s.ExternalTaskID = *externalTask
	}
	if sendingAt != nil {
		s.SendingAt = sendingAt.UTC()
	}
	if resolvedAt != nil {
		s.ResolvedAt = resolvedAt.UTC()
	}
	if observationID != nil {
		s.OutcomeObservationID = *observationID
	}
	if unknownReason != nil {
		s.UnknownReason = *unknownReason
	}
	s.RequestedAt = s.RequestedAt.UTC()
	if !s.SendingAt.IsZero() {
		s.SendingAt = s.SendingAt.UTC()
	}
	if !s.ResolvedAt.IsZero() {
		s.ResolvedAt = s.ResolvedAt.UTC()
	}
	if err := domain.ValidateProviderCancel(s); err != nil {
		return domain.ProviderCancelSnapshot{}, err
	}
	return s, nil
}

const providerCancelColumns = `c.workspace_id,c.run_id,c.attempt_no,c.cancel_key,c.provider_id,c.provider_request_id,c.external_task_id,c.requested_by_subject,c.requested_by_credential,c.reason,c.state,c.requested_at,c.sending_at,c.resolved_at,c.outcome_observation_id,c.unknown_reason`

func loadProviderCancel(ctx context.Context, tx pgx.Tx, workspace domain.WorkspaceID, run domain.RunID, forUpdate bool) (domain.ProviderCancelSnapshot, bool, error) {
	suffix := ""
	if forUpdate {
		suffix = " FOR UPDATE"
	}
	s, err := scanProviderCancel(tx.QueryRow(ctx, `SELECT `+providerCancelColumns+` FROM execution.provider_cancel_intents c WHERE c.workspace_id=$1 AND c.run_id=$2`+suffix, string(workspace), string(run)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProviderCancelSnapshot{}, false, nil
	}
	if err != nil {
		return domain.ProviderCancelSnapshot{}, false, application.ErrProviderCancelUnavailable
	}
	return s, true, nil
}

func cancelTarget(s domain.ProviderCancelSnapshot) application.ProviderCancelTarget {
	return application.ProviderCancelTarget{WorkspaceID: s.WorkspaceID, RunID: s.RunID, AttemptNo: s.AttemptNo, CancelKey: s.CancelKey, ProviderID: s.ProviderID, ProviderRequestID: s.ProviderRequestID, ExternalTaskID: s.ExternalTaskID, RequestedAt: s.RequestedAt, SendingAt: s.SendingAt}
}

func saveProviderCancelIntent(ctx context.Context, tx pgx.Tx, before, after domain.ProviderCancelSnapshot) error {
	tag, err := tx.Exec(ctx, `UPDATE execution.provider_cancel_intents SET state=$1,sending_at=$2,resolved_at=$3,outcome_observation_id=$4,unknown_reason=$5
 WHERE workspace_id=$6 AND run_id=$7 AND state=$8 AND requested_at=$9
   AND sending_at IS NOT DISTINCT FROM $10 AND resolved_at IS NOT DISTINCT FROM $11`, string(after.State), nullableTime(after.SendingAt), nullableTime(after.ResolvedAt), nullableString(after.OutcomeObservationID), nullableString(after.UnknownReason), string(after.WorkspaceID), string(after.RunID), string(before.State), before.RequestedAt, nullableTime(before.SendingAt), nullableTime(before.ResolvedAt))
	if err != nil || tag.RowsAffected() != 1 {
		return application.ErrProviderCancelUnavailable
	}
	return nil
}

func saveProviderCancelRequestedRun(ctx context.Context, tx pgx.Tx, before domain.Snapshot, run domain.Run, caller ports.Caller, reason string) error {
	after := run.Snapshot()
	if after.State != domain.CancelRequested || after.Version != before.Version+1 {
		return application.ErrInvalidProviderCancelResult
	}
	tag, err := tx.Exec(ctx, `UPDATE execution.runs SET state=$1,version=$2,updated_at=$3 WHERE workspace_id=$4 AND id=$5 AND state=$6 AND version=$7 AND updated_at=$8`, string(after.State), int64(after.Version), after.UpdatedAt, string(after.WorkspaceID), string(after.ID), string(before.State), int64(before.Version), before.UpdatedAt)
	if err != nil || tag.RowsAffected() != 1 {
		return application.ErrProviderCancelUnavailable
	}
	_, err = tx.Exec(ctx, `INSERT INTO execution.run_events(workspace_id,run_id,version,state,subject_id,credential_id,occurred_at,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, string(after.WorkspaceID), string(after.ID), int64(after.Version), string(after.State), caller.SubjectID, caller.CredentialID, after.UpdatedAt, reason)
	if err != nil {
		return application.ErrProviderCancelUnavailable
	}
	return nil
}

type providerCancelAttempt struct {
	AttemptNo          uint32
	State              domain.AttemptState
	ProviderID         string
	ProviderRequestID  string
	ExternalTaskID     string
	SubmissionIntentAt time.Time
	SubmittedAt        time.Time
}

func loadCurrentProviderAttempt(ctx context.Context, tx pgx.Tx, job domain.JobSnapshot) (providerCancelAttempt, error) {
	if job.AttemptCount == 0 || job.LeaseGeneration == 0 || uint64(job.AttemptCount) != job.LeaseGeneration {
		return providerCancelAttempt{}, application.ErrInvalidProviderCancelResult
	}
	var attempt providerCancelAttempt
	var attemptNo int32
	var state string
	var externalTask *string
	var submittedAt *time.Time
	err := tx.QueryRow(ctx, `SELECT attempt_no,state,provider_id,provider_request_id,external_task_id,submission_intent_at,submitted_at
 FROM execution.run_attempts WHERE workspace_id=$1 AND run_id=$2 AND attempt_no=$3`, string(job.WorkspaceID), string(job.RunID), int32(job.AttemptCount)).Scan(&attemptNo, &state, &attempt.ProviderID, &attempt.ProviderRequestID, &externalTask, &attempt.SubmissionIntentAt, &submittedAt)
	if err != nil {
		return providerCancelAttempt{}, application.ErrProviderCancelUnavailable
	}
	if attemptNo < 1 {
		return providerCancelAttempt{}, application.ErrInvalidProviderCancelResult
	}
	attempt.AttemptNo, attempt.State = uint32(attemptNo), domain.AttemptState(state)
	if externalTask != nil {
		attempt.ExternalTaskID = *externalTask
	}
	if submittedAt != nil {
		attempt.SubmittedAt = submittedAt.UTC()
	}
	if (attempt.State != domain.AttemptSubmitted && attempt.State != domain.AttemptUnknown) || attempt.ProviderID == "" || attempt.ProviderRequestID == "" {
		return providerCancelAttempt{}, application.ErrInvalidProviderCancelResult
	}
	return attempt, nil
}

func (r *ProviderCancelRequests) RequestProviderCancel(ctx context.Context, caller ports.Caller, runID domain.RunID, reason string, at time.Time) (application.ProviderCancelRequestRecord, error) {
	if r == nil || r.pool == nil || !caller.WorkspaceID.IsValid() || !runID.IsValid() {
		return application.ProviderCancelRequestRecord{}, application.ErrProviderCancelUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return application.ProviderCancelRequestRecord{}, application.ErrProviderCancelUnavailable
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, string(caller.WorkspaceID)); err != nil {
		return application.ProviderCancelRequestRecord{}, application.ErrProviderCancelUnavailable
	}
	run, err := loadRunForUpdate(ctx, tx, caller.WorkspaceID, runID)
	if err != nil {
		return application.ProviderCancelRequestRecord{}, application.ErrProviderCancelUnavailable
	}
	beforeRun := run.Snapshot()
	job, err := loadJobForUpdate(ctx, tx, caller.WorkspaceID, runID)
	if err != nil {
		return application.ProviderCancelRequestRecord{}, application.ErrProviderCancelUnavailable
	}
	attempt, err := loadCurrentProviderAttempt(ctx, tx, job)
	if err != nil {
		return application.ProviderCancelRequestRecord{}, err
	}
	if existing, found, e := loadProviderCancel(ctx, tx, caller.WorkspaceID, runID, false); e != nil {
		return application.ProviderCancelRequestRecord{}, e
	} else if found {
		if beforeRun.State != domain.CancelRequested || existing.AttemptNo != attempt.AttemptNo || existing.ProviderID != attempt.ProviderID || existing.ProviderRequestID != attempt.ProviderRequestID || existing.ExternalTaskID != attempt.ExternalTaskID {
			return application.ProviderCancelRequestRecord{}, application.ErrInvalidProviderCancelResult
		}
		if e = tx.Commit(ctx); e != nil {
			return application.ProviderCancelRequestRecord{}, application.ErrProviderCancelUnavailable
		}
		return application.ProviderCancelRequestRecord{Run: beforeRun, Target: cancelTarget(existing), Replay: true}, nil
	}
	if beforeRun.State != domain.Running && beforeRun.State != domain.Reconciling || job.State != domain.JobProviderWaiting && job.State != domain.JobReconciling {
		return application.ProviderCancelRequestRecord{}, application.ErrInvalidProviderCancelResult
	}
	changed, err := run.RequestProviderCancel(at)
	if err != nil || !changed {
		return application.ProviderCancelRequestRecord{}, application.ErrInvalidProviderCancelResult
	}
	cancelKey := fmt.Sprintf("mender.cancel.%s.%d", runID, attempt.AttemptNo)
	intent, err := domain.NewProviderCancelIntent(domain.ProviderCancelSnapshot{
		WorkspaceID: caller.WorkspaceID, RunID: runID, AttemptNo: attempt.AttemptNo, CancelKey: cancelKey,
		ProviderID: attempt.ProviderID, ProviderRequestID: attempt.ProviderRequestID, ExternalTaskID: attempt.ExternalTaskID,
		RequestedBySubject: caller.SubjectID, RequestedByCredential: caller.CredentialID, Reason: reason, RequestedAt: at,
	})
	if err != nil {
		return application.ProviderCancelRequestRecord{}, application.ErrInvalidProviderCancelResult
	}
	s := intent.Snapshot()
	_, err = tx.Exec(ctx, `INSERT INTO execution.provider_cancel_intents(workspace_id,run_id,attempt_no,cancel_key,provider_id,provider_request_id,external_task_id,requested_by_subject,requested_by_credential,reason,state,requested_at) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10,$11,$12)`, string(s.WorkspaceID), string(s.RunID), int32(s.AttemptNo), s.CancelKey, s.ProviderID, s.ProviderRequestID, s.ExternalTaskID, s.RequestedBySubject, s.RequestedByCredential, s.Reason, string(s.State), s.RequestedAt)
	if err != nil {
		return application.ProviderCancelRequestRecord{}, application.ErrProviderCancelUnavailable
	}
	if err = saveProviderCancelRequestedRun(ctx, tx, beforeRun, run, caller, reason); err != nil {
		return application.ProviderCancelRequestRecord{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ProviderCancelRequestRecord{}, application.ErrProviderCancelUnavailable
	}
	return application.ProviderCancelRequestRecord{Run: run.Snapshot(), Target: cancelTarget(s)}, nil
}

func (r *ProviderCancellations) scoped(ctx context.Context, workspace domain.WorkspaceID) (pgx.Tx, error) {
	if r == nil || r.pool == nil || !workspace.IsValid() {
		return nil, application.ErrProviderCancelUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrProviderCancelUnavailable
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, string(workspace)); err != nil {
		rollback(tx)
		return nil, application.ErrProviderCancelUnavailable
	}
	return tx, nil
}

func sameCancelTarget(s domain.ProviderCancelSnapshot, target application.ProviderCancelTarget) bool {
	return s.WorkspaceID == target.WorkspaceID && s.RunID == target.RunID && s.AttemptNo == target.AttemptNo && s.CancelKey == target.CancelKey && s.ProviderID == target.ProviderID && s.ProviderRequestID == target.ProviderRequestID && s.ExternalTaskID == target.ExternalTaskID
}

func (r *ProviderCancellations) ClaimProviderCancel(ctx context.Context, workspace domain.WorkspaceID, at time.Time) (application.ProviderCancelTarget, bool, error) {
	tx, err := r.scoped(ctx, workspace)
	if err != nil {
		return application.ProviderCancelTarget{}, false, err
	}
	defer rollback(tx)
	s, err := scanProviderCancel(tx.QueryRow(ctx, `SELECT `+providerCancelColumns+` FROM execution.provider_cancel_intents c JOIN execution.runs r ON (r.workspace_id,r.id)=(c.workspace_id,c.run_id) JOIN execution.jobs j ON (j.workspace_id,j.run_id)=(c.workspace_id,c.run_id) WHERE c.workspace_id=$1 AND c.state='requested' AND r.state='cancel_requested' AND j.state IN ('provider_waiting','reconciling') ORDER BY c.requested_at,c.run_id LIMIT 1 FOR UPDATE OF c SKIP LOCKED`, string(workspace)))
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Commit(ctx); err != nil {
			return application.ProviderCancelTarget{}, false, application.ErrProviderCancelUnavailable
		}
		return application.ProviderCancelTarget{}, false, nil
	}
	if err != nil {
		return application.ProviderCancelTarget{}, false, application.ErrProviderCancelUnavailable
	}
	intent, _ := domain.RestoreProviderCancelIntent(s)
	before := intent.Snapshot()
	if err = intent.Claim(at); err != nil {
		return application.ProviderCancelTarget{}, false, application.ErrInvalidProviderCancelResult
	}
	after := intent.Snapshot()
	if err = saveProviderCancelIntent(ctx, tx, before, after); err != nil {
		return application.ProviderCancelTarget{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ProviderCancelTarget{}, false, application.ErrProviderCancelUnavailable
	}
	return cancelTarget(after), true, nil
}

func (r *ProviderCancellations) RecordProviderCancelUnknown(ctx context.Context, target application.ProviderCancelTarget, at time.Time, reason string) error {
	tx, err := r.scoped(ctx, target.WorkspaceID)
	if err != nil {
		return err
	}
	defer rollback(tx)
	run, err := loadRunForUpdate(ctx, tx, target.WorkspaceID, target.RunID)
	if err != nil || run.Snapshot().State != domain.CancelRequested {
		return application.ErrProviderCancelUnavailable
	}
	job, err := loadJobForUpdate(ctx, tx, target.WorkspaceID, target.RunID)
	if err != nil || job.State == domain.JobFinished {
		return application.ErrProviderAlreadyTerminal
	}
	s, found, err := loadProviderCancel(ctx, tx, target.WorkspaceID, target.RunID, true)
	if err != nil || !found || !sameCancelTarget(s, target) || s.State != domain.ProviderCancelSending {
		return application.ErrProviderCancelUnavailable
	}
	intent, _ := domain.RestoreProviderCancelIntent(s)
	before := intent.Snapshot()
	if err = intent.MarkUnknown(at, reason); err != nil {
		return application.ErrInvalidProviderCancelResult
	}
	if err = saveProviderCancelIntent(ctx, tx, before, intent.Snapshot()); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ErrProviderCancelUnavailable
	}
	return nil
}

func resolveCancelIntentForObservation(ctx context.Context, tx pgx.Tx, observation domain.ProviderObservation) error {
	s, found, err := loadProviderCancel(ctx, tx, observation.WorkspaceID, observation.RunID, true)
	if err != nil || !found {
		return err
	}
	intent, err := domain.RestoreProviderCancelIntent(s)
	if err != nil {
		return application.ErrProviderCancelUnavailable
	}
	before := intent.Snapshot()
	if err = intent.ResolveFromProvider(observation.State, observation.ObservedAt, observation.ObservationID); err != nil {
		return application.ErrProviderAlreadyTerminal
	}
	return saveProviderCancelIntent(ctx, tx, before, intent.Snapshot())
}

func (r *ProviderCancellations) RecordProviderCancelAcknowledged(ctx context.Context, target application.ProviderCancelTarget, observationID string, observedAt time.Time) (application.ProviderResultRecord, error) {
	tx, err := r.scoped(ctx, target.WorkspaceID)
	if err != nil {
		return application.ProviderResultRecord{}, err
	}
	defer rollback(tx)
	run, err := loadRunForUpdate(ctx, tx, target.WorkspaceID, target.RunID)
	if err != nil {
		return application.ProviderResultRecord{}, application.ErrProviderCancelUnavailable
	}
	beforeRun := run.Snapshot()
	if beforeRun.State.IsTerminal() {
		return application.ProviderResultRecord{}, application.ErrProviderAlreadyTerminal
	}
	if beforeRun.State != domain.CancelRequested {
		return application.ProviderResultRecord{}, application.ErrInvalidProviderCancelResult
	}
	beforeJob, err := loadJobForUpdate(ctx, tx, target.WorkspaceID, target.RunID)
	if err != nil || beforeJob.State == domain.JobFinished {
		return application.ProviderResultRecord{}, application.ErrProviderAlreadyTerminal
	}
	s, found, err := loadProviderCancel(ctx, tx, target.WorkspaceID, target.RunID, true)
	if err != nil || !found || !sameCancelTarget(s, target) || s.State != domain.ProviderCancelSending || observedAt.Before(s.SendingAt) {
		return application.ProviderResultRecord{}, application.ErrInvalidProviderCancelResult
	}
	attempt, err := loadCurrentProviderAttempt(ctx, tx, beforeJob)
	if err != nil || attempt.AttemptNo != target.AttemptNo || attempt.ProviderID != target.ProviderID || attempt.ProviderRequestID != target.ProviderRequestID || attempt.ExternalTaskID != target.ExternalTaskID {
		return application.ProviderResultRecord{}, application.ErrInvalidProviderCancelResult
	}
	observation := domain.ProviderObservation{WorkspaceID: target.WorkspaceID, RunID: target.RunID, ObservationID: observationID, AttemptNo: target.AttemptNo, ProviderID: target.ProviderID, ProviderRequestID: target.ProviderRequestID, ExternalTaskID: target.ExternalTaskID, State: domain.ProviderCanceled, ObservedAt: observedAt}
	if observation.Validate() != nil || observedAt.Before(attempt.SubmissionIntentAt) {
		return application.ProviderResultRecord{}, application.ErrInvalidProviderCancelResult
	}
	intent, _ := domain.RestoreProviderCancelIntent(s)
	beforeIntent := intent.Snapshot()
	if err = intent.ResolveFromProvider(domain.ProviderCanceled, observedAt, observationID); err != nil {
		return application.ProviderResultRecord{}, application.ErrInvalidProviderCancelResult
	}
	job, _ := domain.RestoreJob(beforeJob)
	if err = job.FinishFromProvider(observedAt); err != nil {
		return application.ProviderResultRecord{}, application.ErrInvalidProviderCancelResult
	}
	if err = run.ConfirmCanceled(observedAt); err != nil {
		return application.ProviderResultRecord{}, application.ErrInvalidProviderCancelResult
	}
	if err = insertProviderObservation(ctx, tx, observation); err != nil {
		return application.ProviderResultRecord{}, err
	}
	if err = saveProviderCancelIntent(ctx, tx, beforeIntent, intent.Snapshot()); err != nil {
		return application.ProviderResultRecord{}, err
	}
	afterJob := job.Snapshot()
	if err = saveFinishedJob(ctx, tx, beforeJob, afterJob); err != nil {
		return application.ProviderResultRecord{}, err
	}
	if err = saveProviderRun(ctx, tx, beforeRun, run, "provider cancellation acknowledged"); err != nil {
		return application.ProviderResultRecord{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ProviderResultRecord{}, application.ErrProviderCancelUnavailable
	}
	return application.ProviderResultRecord{Run: run.Snapshot(), Job: afterJob, Observation: observation}, nil
}

var _ application.ProviderCancelRequestRepository = (*ProviderCancelRequests)(nil)
var _ application.ProviderCancelControlRepository = (*ProviderCancellations)(nil)
