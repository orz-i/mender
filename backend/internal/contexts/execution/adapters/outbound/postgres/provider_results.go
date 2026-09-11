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

type ProviderResults struct{ pool *pgxpool.Pool }

func NewProviderResults(pool *pgxpool.Pool) *ProviderResults { return &ProviderResults{pool: pool} }

func (r *ProviderResults) scoped(ctx context.Context, workspace domain.WorkspaceID) (pgx.Tx, error) {
	if r == nil || r.pool == nil || !workspace.IsValid() {
		return nil, application.ErrProviderResultUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrProviderResultUnavailable
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, string(workspace)); err != nil {
		rollback(tx)
		return nil, application.ErrProviderResultUnavailable
	}
	return tx, nil
}

func scanProviderObservation(row rowScanner) (domain.ProviderObservation, error) {
	var o domain.ProviderObservation
	var workspace, run, state string
	var attempt int32
	var providerID, externalTask, resultJSON, errorCode *string
	if err := row.Scan(&workspace, &run, &o.ObservationID, &attempt, &providerID, &o.ProviderRequestID, &externalTask, &state, &resultJSON, &errorCode, &o.ObservedAt); err != nil {
		return domain.ProviderObservation{}, err
	}
	o.WorkspaceID, o.RunID = domain.WorkspaceID(workspace), domain.RunID(run)
	o.AttemptNo, o.State = uint32(attempt), domain.ProviderResultState(state)
	if providerID != nil {
		o.ProviderID = *providerID
	}
	if externalTask != nil {
		o.ExternalTaskID = *externalTask
	}
	if resultJSON != nil {
		o.ResultJSON = *resultJSON
	}
	if errorCode != nil {
		o.ErrorCode = *errorCode
	}
	o.ObservedAt = o.ObservedAt.UTC()
	if err := o.Validate(); err != nil {
		return domain.ProviderObservation{}, err
	}
	return o, nil
}

func loadProviderObservation(ctx context.Context, tx pgx.Tx, o domain.ProviderObservation) (domain.ProviderObservation, bool, error) {
	row := tx.QueryRow(ctx, `SELECT workspace_id,run_id,observation_id,attempt_no,provider_id,provider_request_id,external_task_id,state,result_json::text,error_code,observed_at
 FROM execution.provider_observations WHERE workspace_id=$1 AND run_id=$2 AND observation_id=$3`, string(o.WorkspaceID), string(o.RunID), o.ObservationID)
	existing, err := scanProviderObservation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProviderObservation{}, false, nil
	}
	if err != nil {
		return domain.ProviderObservation{}, false, application.ErrProviderResultUnavailable
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT attempt_no=$4 AND provider_id IS NOT DISTINCT FROM NULLIF($5,'') AND provider_request_id=$6 AND external_task_id IS NOT DISTINCT FROM NULLIF($7,'') AND state=$8
 AND result_json IS NOT DISTINCT FROM CASE WHEN $9='' THEN NULL ELSE $9::jsonb END
 AND error_code IS NOT DISTINCT FROM NULLIF($10,'') AND observed_at=$11
 FROM execution.provider_observations WHERE workspace_id=$1 AND run_id=$2 AND observation_id=$3`, string(o.WorkspaceID), string(o.RunID), o.ObservationID, int32(o.AttemptNo), o.ProviderID, o.ProviderRequestID, o.ExternalTaskID, string(o.State), o.ResultJSON, o.ErrorCode, o.ObservedAt).Scan(&same)
	if err != nil {
		return domain.ProviderObservation{}, false, application.ErrProviderResultUnavailable
	}
	if !same {
		return domain.ProviderObservation{}, false, application.ErrProviderResultConflict
	}
	return existing, true, nil
}

func saveProviderRun(ctx context.Context, tx pgx.Tx, before domain.Snapshot, run domain.Run, reason string) error {
	after := run.Snapshot()
	if after.Version != before.Version+1 || after.WorkspaceID != before.WorkspaceID || after.ID != before.ID {
		return application.ErrProviderResultConflict
	}
	tag, err := tx.Exec(ctx, `UPDATE execution.runs SET state=$1,version=$2,updated_at=$3 WHERE workspace_id=$4 AND id=$5 AND state=$6 AND version=$7 AND updated_at=$8`, string(after.State), int64(after.Version), after.UpdatedAt, string(after.WorkspaceID), string(after.ID), string(before.State), int64(before.Version), before.UpdatedAt)
	if err != nil || tag.RowsAffected() != 1 {
		return application.ErrProviderResultConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO execution.run_events(workspace_id,run_id,version,state,subject_id,credential_id,occurred_at,reason) VALUES($1,$2,$3,$4,'provider_reconciler','provider_result',$5,$6)`, string(after.WorkspaceID), string(after.ID), int64(after.Version), string(after.State), after.UpdatedAt, reason)
	if err != nil {
		return application.ErrProviderResultUnavailable
	}
	return nil
}

func insertSettlementJob(ctx context.Context, tx pgx.Tx, o domain.ProviderObservation) error {
	if !o.IsTerminal() {
		return application.ErrProviderResultConflict
	}
	_, err := tx.Exec(ctx, `INSERT INTO execution.settlement_jobs(workspace_id,run_id,observation_id,created_at) VALUES($1,$2,$3,$4)`, string(o.WorkspaceID), string(o.RunID), o.ObservationID, o.ObservedAt)
	if err != nil {
		return application.ErrProviderResultUnavailable
	}
	return nil
}

func saveFinishedJob(ctx context.Context, tx pgx.Tx, before, after domain.JobSnapshot) error {
	tag, err := tx.Exec(ctx, `UPDATE execution.jobs SET state=$1,blocked_reason=NULLIF($2,''),updated_at=$3,stopped_at=$4
 WHERE workspace_id=$5 AND run_id=$6 AND state=$7 AND lease_generation=$8 AND updated_at=$9`, string(after.State), after.BlockedReason, after.UpdatedAt, after.StoppedAt, string(after.WorkspaceID), string(after.RunID), string(before.State), int64(before.LeaseGeneration), before.UpdatedAt)
	if err != nil || tag.RowsAffected() != 1 {
		return application.ErrProviderResultConflict
	}
	return nil
}

func insertProviderObservation(ctx context.Context, tx pgx.Tx, o domain.ProviderObservation) error {
	_, err := tx.Exec(ctx, `INSERT INTO execution.provider_observations(workspace_id,run_id,observation_id,attempt_no,provider_id,provider_request_id,external_task_id,state,result_json,error_code,observed_at)
 VALUES($1,$2,$3,$4,NULLIF($5,''),$6,NULLIF($7,''),$8,CASE WHEN $9='' THEN NULL ELSE $9::jsonb END,NULLIF($10,''),$11)`, string(o.WorkspaceID), string(o.RunID), o.ObservationID, int32(o.AttemptNo), o.ProviderID, o.ProviderRequestID, o.ExternalTaskID, string(o.State), o.ResultJSON, o.ErrorCode, o.ObservedAt)
	if err != nil {
		return application.ErrProviderResultUnavailable
	}
	return nil
}

func (r *ProviderResults) RecordProviderObservation(ctx context.Context, observation domain.ProviderObservation) (application.ProviderResultRecord, error) {
	if observation.Validate() != nil {
		return application.ProviderResultRecord{}, domain.ErrInvalidProviderObservation
	}
	tx, err := r.scoped(ctx, observation.WorkspaceID)
	if err != nil {
		return application.ProviderResultRecord{}, err
	}
	defer rollback(tx)
	if existing, found, e := loadProviderObservation(ctx, tx, observation); e != nil {
		return application.ProviderResultRecord{}, e
	} else if found {
		run, e := loadRunForUpdate(ctx, tx, observation.WorkspaceID, observation.RunID)
		if e != nil {
			return application.ProviderResultRecord{}, e
		}
		job, e := loadJobForUpdate(ctx, tx, observation.WorkspaceID, observation.RunID)
		if e != nil {
			return application.ProviderResultRecord{}, e
		}
		if e = tx.Commit(ctx); e != nil {
			return application.ProviderResultRecord{}, application.ErrProviderResultUnavailable
		}
		return application.ProviderResultRecord{Run: run.Snapshot(), Job: job, Observation: existing}, nil
	}
	run, err := loadRunForUpdate(ctx, tx, observation.WorkspaceID, observation.RunID)
	if err != nil {
		return application.ProviderResultRecord{}, application.ErrProviderResultUnavailable
	}
	beforeRun := run.Snapshot()
	beforeJob, err := loadJobForUpdate(ctx, tx, observation.WorkspaceID, observation.RunID)
	if err != nil {
		return application.ProviderResultRecord{}, application.ErrProviderResultUnavailable
	}
	attempt, err := scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptColumns+` FROM execution.run_attempts a WHERE a.workspace_id=$1 AND a.run_id=$2 AND a.attempt_no=$3`, string(observation.WorkspaceID), string(observation.RunID), int32(observation.AttemptNo)))
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ProviderResultRecord{}, application.ErrProviderResultConflict
	}
	if err != nil {
		return application.ProviderResultRecord{}, application.ErrProviderResultUnavailable
	}
	if (attempt.State != domain.AttemptSubmitted && attempt.State != domain.AttemptUnknown) || attempt.ProviderID == "" || attempt.ProviderID != observation.ProviderID || attempt.ProviderRequestID == "" || attempt.ProviderRequestID != observation.ProviderRequestID || observation.ExternalTaskID != "" && attempt.ExternalTaskID != observation.ExternalTaskID {
		return application.ProviderResultRecord{}, application.ErrProviderResultConflict
	}
	evidenceAt := attempt.SubmissionIntentAt
	if !attempt.SubmittedAt.IsZero() {
		evidenceAt = attempt.SubmittedAt
	}
	if observation.ObservedAt.Before(evidenceAt) {
		return application.ProviderResultRecord{}, application.ErrProviderResultConflict
	}
	var latest *time.Time
	if err = tx.QueryRow(ctx, `SELECT max(observed_at) FROM execution.provider_observations WHERE workspace_id=$1 AND run_id=$2`, string(observation.WorkspaceID), string(observation.RunID)).Scan(&latest); err != nil {
		return application.ProviderResultRecord{}, application.ErrProviderResultUnavailable
	}
	if latest != nil && observation.ObservedAt.Before(*latest) {
		return application.ProviderResultRecord{}, application.ErrProviderResultConflict
	}
	if beforeRun.State.IsTerminal() || beforeJob.State == domain.JobFinished {
		return application.ProviderResultRecord{}, application.ErrProviderAlreadyTerminal
	}
	if !observation.IsTerminal() {
		if (beforeRun.State != domain.Running && beforeRun.State != domain.Reconciling && beforeRun.State != domain.CancelRequested) || (beforeJob.State != domain.JobProviderWaiting && beforeJob.State != domain.JobReconciling) {
			return application.ProviderResultRecord{}, application.ErrProviderResultConflict
		}
		if err = insertProviderObservation(ctx, tx, observation); err != nil {
			return application.ProviderResultRecord{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return application.ProviderResultRecord{}, application.ErrProviderResultUnavailable
		}
		return application.ProviderResultRecord{Run: beforeRun, Job: beforeJob, Observation: observation}, nil
	}
	job, _ := domain.RestoreJob(beforeJob)
	if err = job.FinishFromProvider(observation.ObservedAt); err != nil {
		return application.ProviderResultRecord{}, application.ErrProviderResultConflict
	}
	switch observation.State {
	case domain.ProviderSucceeded:
		err = run.ConfirmSucceeded(observation.ObservedAt)
	case domain.ProviderFailed:
		err = run.ConfirmFailed(observation.ObservedAt)
	case domain.ProviderCanceled:
		err = run.ConfirmCanceled(observation.ObservedAt)
	default:
		err = domain.ErrInvalidProviderObservation
	}
	if err != nil {
		return application.ProviderResultRecord{}, application.ErrProviderResultConflict
	}
	if err = resolveCancelIntentForObservation(ctx, tx, observation); err != nil {
		return application.ProviderResultRecord{}, err
	}
	if err = insertProviderObservation(ctx, tx, observation); err != nil {
		return application.ProviderResultRecord{}, err
	}
	if err = insertSettlementJob(ctx, tx, observation); err != nil {
		return application.ProviderResultRecord{}, err
	}
	afterJob := job.Snapshot()
	if err = saveFinishedJob(ctx, tx, beforeJob, afterJob); err != nil {
		return application.ProviderResultRecord{}, err
	}
	if err = saveProviderRun(ctx, tx, beforeRun, run, fmt.Sprintf("provider result %s", observation.State)); err != nil {
		return application.ProviderResultRecord{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.ProviderResultRecord{}, application.ErrProviderResultUnavailable
	}
	return application.ProviderResultRecord{Run: run.Snapshot(), Job: afterJob, Observation: observation}, nil
}

var _ application.ProviderResultRepository = (*ProviderResults)(nil)
