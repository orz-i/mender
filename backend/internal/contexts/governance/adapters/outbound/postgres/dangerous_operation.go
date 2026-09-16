package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type DangerousOperationRepository struct{ pool *pgxpool.Pool }

func NewDangerousOperation(pool *pgxpool.Pool) *DangerousOperationRepository {
	return &DangerousOperationRepository{pool: pool}
}

func dangerousError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "22023":
			return application.ErrInvalid
		case "23505", "23514", "40001":
			return application.ErrConflict
		case "42501":
			return application.ErrForbidden
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return application.ErrUnavailable
}

func (r *DangerousOperationRepository) begin(ctx context.Context, workspace string) (pgx.Tx, error) {
	if r == nil || r.pool == nil || workspace == "" {
		return nil, application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		rollback(tx)
		return nil, application.ErrUnavailable
	}
	return tx, nil
}

const dangerousColumns = `workspace_id,id,requester_user_id,subject_kind,subject_id,action,target_kind,target_id,target_version,parameters_json::text,parameters_sha256,amount_micro,coalesce(currency,''),reason,state,requested_at,expires_at,coalesce(reviewer_user_id,''),reviewed_at,decision_note,consumed_at`

func scanDangerous(row pgx.Row) (application.DangerousOperationApproval, error) {
	var item application.DangerousOperationApproval
	var reviewedAt, consumedAt *time.Time
	err := row.Scan(&item.WorkspaceID, &item.ID, &item.RequesterUserID, &item.SubjectKind, &item.SubjectID, &item.Action, &item.TargetKind, &item.TargetID, &item.TargetVersion,
		&item.ParametersJSON, &item.ParametersSHA256, &item.AmountMicro, &item.Currency, &item.Reason, &item.State, &item.RequestedAt, &item.ExpiresAt,
		&item.ReviewerUserID, &reviewedAt, &item.DecisionNote, &consumedAt)
	if reviewedAt != nil {
		item.ReviewedAt = *reviewedAt
	}
	if consumedAt != nil {
		item.ConsumedAt = *consumedAt
	}
	return item, dangerousError(err)
}

func (r *DangerousOperationRepository) ListDangerousOperations(ctx context.Context, workspace string, at time.Time) ([]application.DangerousOperationApproval, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	rows, err := tx.Query(ctx, `SELECT workspace_id,id,requester_user_id,subject_kind,subject_id,action,target_kind,target_id,target_version,parameters_json::text,parameters_sha256,amount_micro,coalesce(currency,''),reason,CASE WHEN state IN ('pending','approved') AND expires_at<=$2 THEN 'expired' ELSE state END,requested_at,expires_at,coalesce(reviewer_user_id,''),reviewed_at,decision_note,consumed_at FROM governance.dangerous_operation_approvals WHERE workspace_id=$1 ORDER BY requested_at DESC,id DESC LIMIT 100`, workspace, at)
	if err != nil {
		return nil, dangerousError(err)
	}
	defer rows.Close()
	items := []application.DangerousOperationApproval{}
	for rows.Next() {
		item, scanErr := scanDangerous(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, application.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, application.ErrUnavailable
	}
	return items, nil
}

func (r *DangerousOperationRepository) find(ctx context.Context, tx pgx.Tx, workspace, id string) (application.DangerousOperationApproval, error) {
	return scanDangerous(tx.QueryRow(ctx, `SELECT `+dangerousColumns+` FROM governance.dangerous_operation_approvals WHERE workspace_id=$1 AND id=$2`, workspace, id))
}

func (r *DangerousOperationRepository) RequestReleaseEmergency(ctx context.Context, workspace, id, requester, releaseID, reason string, at, expires time.Time) (application.DangerousOperationApproval, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.DangerousOperationApproval{}, err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT governance.request_release_emergency_approval($1,$2,$3,$4,$5,$6,$7)`, workspace, id, requester, releaseID, reason, at, expires); err != nil {
		return application.DangerousOperationApproval{}, dangerousError(err)
	}
	item, err := r.find(ctx, tx, workspace, id)
	if err != nil {
		return item, err
	}
	if err = tx.Commit(ctx); err != nil {
		return item, application.ErrUnavailable
	}
	return item, nil
}

func (r *DangerousOperationRepository) decide(ctx context.Context, workspace, id, reviewer string, at time.Time, note, function string) (application.DangerousOperationApproval, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.DangerousOperationApproval{}, err
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, function, workspace, id, reviewer, at, note); err != nil {
		return application.DangerousOperationApproval{}, dangerousError(err)
	}
	item, err := r.find(ctx, tx, workspace, id)
	if err != nil {
		return item, err
	}
	if err = tx.Commit(ctx); err != nil {
		return item, application.ErrUnavailable
	}
	return item, nil
}

func (r *DangerousOperationRepository) ApproveDangerousOperation(ctx context.Context, workspace, id, reviewer string, at time.Time, note string) (application.DangerousOperationApproval, error) {
	return r.decide(ctx, workspace, id, reviewer, at, note, `SELECT governance.approve_dangerous_operation($1,$2,$3,$4,$5)`)
}

func (r *DangerousOperationRepository) RejectDangerousOperation(ctx context.Context, workspace, id, reviewer string, at time.Time, note string) (application.DangerousOperationApproval, error) {
	return r.decide(ctx, workspace, id, reviewer, at, note, `SELECT governance.reject_dangerous_operation($1,$2,$3,$4,$5)`)
}

var _ application.DangerousOperationRepository = (*DangerousOperationRepository)(nil)
