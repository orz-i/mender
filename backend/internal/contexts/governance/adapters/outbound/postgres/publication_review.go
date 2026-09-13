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

type PublicationReviewRepository struct{ pool *pgxpool.Pool }

func NewPublicationReview(pool *pgxpool.Pool) *PublicationReviewRepository {
	return &PublicationReviewRepository{pool: pool}
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505", "23514":
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

func (r *PublicationReviewRepository) begin(ctx context.Context, workspace string) (pgx.Tx, error) {
	if r == nil || r.pool == nil {
		return nil, application.ErrUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrUnavailable
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('mender.workspace_id',$1,true)", workspace); err != nil {
		rollback(tx)
		return nil, application.ErrUnavailable
	}
	return tx, nil
}

const approvalCols = `workspace_id,id,target_kind,target_id,target_revision,requester_user_id,state,requested_at,expires_at,coalesce(reviewer_user_id,''),reviewed_at,decision_note,consumed_at`

func scan(row pgx.Row) (application.PublicationApproval, error) {
	var a application.PublicationApproval
	var reviewed, consumed *time.Time
	err := row.Scan(&a.WorkspaceID, &a.ID, &a.TargetKind, &a.TargetID, &a.TargetRevision, &a.RequesterUserID, &a.State, &a.RequestedAt, &a.ExpiresAt, &a.ReviewerUserID, &reviewed, &a.DecisionNote, &consumed)
	if reviewed != nil {
		a.ReviewedAt = *reviewed
	}
	if consumed != nil {
		a.ConsumedAt = *consumed
	}
	return a, mapErr(err)
}

func (r *PublicationReviewRepository) ListPublicationApprovals(ctx context.Context, workspace string, at time.Time) ([]application.PublicationApproval, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	rows, err := tx.Query(ctx, `SELECT workspace_id,id,target_kind,target_id,target_revision,requester_user_id,CASE WHEN state IN ('pending','approved') AND expires_at<=$2 THEN 'expired' ELSE state END,requested_at,expires_at,coalesce(reviewer_user_id,''),reviewed_at,decision_note,consumed_at FROM governance.catalog_publication_approvals WHERE workspace_id=$1 ORDER BY requested_at DESC,id`, workspace, at)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	items := []application.PublicationApproval{}
	for rows.Next() {
		a, e := scan(rows)
		if e != nil {
			return nil, e
		}
		items = append(items, a)
	}
	if rows.Err() != nil {
		return nil, application.ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, application.ErrUnavailable
	}
	return items, nil
}

func (r *PublicationReviewRepository) ApprovePublication(ctx context.Context, workspace, id, reviewer, note string, at time.Time) (application.PublicationApproval, error) {
	return r.decide(ctx, workspace, id, reviewer, note, at, true)
}
func (r *PublicationReviewRepository) RejectPublication(ctx context.Context, workspace, id, reviewer, note string, at time.Time) (application.PublicationApproval, error) {
	return r.decide(ctx, workspace, id, reviewer, note, at, false)
}
func (r *PublicationReviewRepository) decide(ctx context.Context, workspace, id, reviewer, note string, at time.Time, approve bool) (application.PublicationApproval, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return application.PublicationApproval{}, err
	}
	defer rollback(tx)
	statement := `SELECT governance.reject_catalog_publication($1,$2,$3,$4,$5)`
	if approve {
		statement = `SELECT governance.approve_catalog_publication($1,$2,$3,$4,$5)`
	}
	if _, err = tx.Exec(ctx, statement, workspace, id, reviewer, at, note); err != nil {
		return application.PublicationApproval{}, mapErr(err)
	}
	a, err := scan(tx.QueryRow(ctx, `SELECT `+approvalCols+` FROM governance.catalog_publication_approvals WHERE workspace_id=$1 AND id=$2`, workspace, id))
	if err != nil {
		return application.PublicationApproval{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return application.PublicationApproval{}, application.ErrUnavailable
	}
	return a, nil
}

var _ application.Repository = (*PublicationReviewRepository)(nil)
