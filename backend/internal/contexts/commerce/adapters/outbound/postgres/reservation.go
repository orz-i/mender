package postgres

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/domain"
)

// This adapter only touches commerce-owned tables. Transaction lifetime belongs to the UoW.
type Reservations struct{ tx pgx.Tx }

func NewReservations(tx pgx.Tx) *Reservations { return &Reservations{tx: tx} }
func (r *Reservations) LoadForUpdate(ctx context.Context, q application.ReserveRequest) (application.BudgetPeriod, error) {
	var p application.BudgetPeriod
	var s domain.AllowanceSnapshot
	err := r.tx.QueryRow(ctx, `SELECT workspace_id,budget_id,period_id,currency,starts_at,ends_at,active,limit_micro,consumed_micro,reserved_micro,revision FROM commerce.budget_periods WHERE workspace_id=$1 AND budget_id=$2 AND period_id=$3 FOR UPDATE`, q.WorkspaceID, q.BudgetID, q.PeriodID).Scan(&p.WorkspaceID, &p.BudgetID, &p.PeriodID, &p.Currency, &p.StartsAt, &p.EndsAt, &p.Active, &s.Limit, &s.Consumed, &s.Reserved, &s.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, application.ErrBudgetUnavailable
	}
	if err != nil {
		return p, application.ErrReservationUnavailable
	}
	// Evaluate database time only after the row lock was acquired, not before waiting for it.
	if err = r.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&p.CheckedAt); err != nil {
		return p, application.ErrReservationUnavailable
	}
	p.Allowance, err = domain.RestoreAllowance(s)
	if err != nil {
		return p, application.ErrBudgetUnavailable
	}
	return p, nil
}
func (r *Reservations) StoreReservation(ctx context.Context, q application.ReserveRequest, a domain.Allowance, expected int64) error {
	s := a.Snapshot()
	tag, err := r.tx.Exec(ctx, `UPDATE commerce.budget_periods SET reserved_micro=$1,revision=$2 WHERE workspace_id=$3 AND budget_id=$4 AND period_id=$5 AND revision=$6`, s.Reserved, s.Revision, q.WorkspaceID, q.BudgetID, q.PeriodID, expected)
	if err != nil || tag.RowsAffected() != 1 {
		return application.ErrReservationUnavailable
	}
	_, err = r.tx.Exec(ctx, `INSERT INTO commerce.reservations(workspace_id,id,run_id,budget_id,period_id,currency,amount_micro,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, q.WorkspaceID, q.ReservationID, q.RunID, q.BudgetID, q.PeriodID, q.Currency, q.AmountMicro, q.At)
	if err != nil {
		return application.ErrReservationUnavailable
	}
	return nil
}

var _ application.ReservationRepository = (*Reservations)(nil)
