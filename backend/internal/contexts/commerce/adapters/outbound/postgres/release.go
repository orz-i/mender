package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/domain"
	"time"
)

type Releases struct{ tx pgx.Tx }

func NewReleases(tx pgx.Tx) *Releases { return &Releases{tx} }
func (r *Releases) LockRelease(ctx context.Context, q application.ReleaseRequest) (application.LockedRelease, error) {
	var a domain.AllowanceSnapshot
	var currency string
	err := r.tx.QueryRow(ctx, `SELECT currency,limit_micro,consumed_micro,reserved_micro,revision FROM commerce.budget_periods WHERE workspace_id=$1 AND budget_id=$2 AND period_id=$3 FOR UPDATE`, q.WorkspaceID, q.BudgetID, q.PeriodID).Scan(&currency, &a.Limit, &a.Consumed, &a.Reserved, &a.Revision)
	if err != nil {
		return application.LockedRelease{}, application.ErrReservationUnavailable
	}
	var v application.LockedRelease
	var at *time.Time
	err = r.tx.QueryRow(ctx, `SELECT workspace_id,run_id,id,budget_id,period_id,currency,amount_micro,state,created_at,released_at FROM commerce.reservations WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, q.WorkspaceID, q.ReservationID).Scan(&v.Reference.WorkspaceID, &v.Reference.RunID, &v.Reference.ReservationID, &v.Reference.BudgetID, &v.Reference.PeriodID, &v.Reference.Currency, &v.Reference.AmountMicro, &v.State, &v.CreatedAt, &at)
	if err != nil {
		return v, application.ErrReservationUnavailable
	}
	if currency != q.Currency {
		return v, application.ErrReleaseConflict
	}
	if at != nil {
		v.ReleasedAt = *at
	}
	v.Allowance, err = domain.RestoreAllowance(a)
	if err != nil {
		return v, application.ErrReleaseConflict
	}
	return v, nil
}
func (r *Releases) StoreRelease(ctx context.Context, q application.ReleaseRequest, a domain.Allowance, expected int64, at time.Time) error {
	s := a.Snapshot()
	tag, e := r.tx.Exec(ctx, `UPDATE commerce.budget_periods SET reserved_micro=$1,revision=$2 WHERE workspace_id=$3 AND budget_id=$4 AND period_id=$5 AND revision=$6`, s.Reserved, s.Revision, q.WorkspaceID, q.BudgetID, q.PeriodID, expected)
	if e != nil || tag.RowsAffected() != 1 {
		return application.ErrReservationUnavailable
	}
	tag, e = r.tx.Exec(ctx, `UPDATE commerce.reservations SET state='released',released_at=$1 WHERE workspace_id=$2 AND id=$3 AND run_id=$4 AND budget_id=$5 AND period_id=$6 AND currency=$7 AND amount_micro=$8 AND state='held' AND released_at IS NULL AND created_at<=$1`, at, q.WorkspaceID, q.ReservationID, q.RunID, q.BudgetID, q.PeriodID, q.Currency, q.AmountMicro)
	if e != nil || tag.RowsAffected() != 1 {
		return application.ErrReservationUnavailable
	}
	return nil
}

var _ application.ReleaseRepository = (*Releases)(nil)
