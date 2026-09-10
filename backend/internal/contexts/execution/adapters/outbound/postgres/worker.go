package postgres

import (
	"context"
	"errors"
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

type rowScanner interface{ Scan(...any) error }

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
	tag, err := tx.Exec(ctx, `UPDATE execution.run_attempts SET lease_until=$1 WHERE workspace_id=$2 AND run_id=$3 AND lease_generation=$4 AND lease_owner=$5 AND state='leased' AND lease_until=$6`, after.LeaseUntil, string(token.WorkspaceID), string(token.RunID), int64(token.Generation), token.WorkerID, before.LeaseUntil)
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

func (w *Workers) RecoverExpired(ctx context.Context, workspace domain.WorkspaceID, at time.Time) (domain.JobSnapshot, bool, error) {
	tx, err := w.scoped(ctx, workspace)
	if err != nil {
		return domain.JobSnapshot{}, false, err
	}
	defer rollback(tx)
	row := tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM execution.jobs j WHERE j.workspace_id=$1 AND j.state='leased' AND j.lease_until<=$2
 ORDER BY j.lease_until,j.run_id FOR UPDATE SKIP LOCKED LIMIT 1`, string(workspace), at)
	before, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JobSnapshot{}, false, nil
	}
	if err != nil {
		return domain.JobSnapshot{}, false, application.ErrWorkerUnavailable
	}
	job, _ := domain.RestoreJob(before)
	if err = job.RecoverExpired(at); err != nil {
		return domain.JobSnapshot{}, false, err
	}
	after := job.Snapshot()
	if err = updateJob(ctx, tx, before, after); err != nil {
		return domain.JobSnapshot{}, false, err
	}
	tag, err := tx.Exec(ctx, `UPDATE execution.run_attempts SET state='expired',finished_at=$1 WHERE workspace_id=$2 AND run_id=$3 AND lease_generation=$4 AND lease_owner=$5 AND state='leased'`, at, string(workspace), string(before.RunID), int64(before.LeaseGeneration), before.LeaseOwner)
	if err != nil || tag.RowsAffected() != 1 {
		return domain.JobSnapshot{}, false, application.ErrWorkerUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.JobSnapshot{}, false, application.ErrWorkerUnavailable
	}
	return after, true, nil
}

var _ application.WorkerRepository = (*Workers)(nil)
