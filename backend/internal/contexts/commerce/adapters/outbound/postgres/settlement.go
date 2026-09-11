package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/domain"
)

type Settlements struct{ tx pgx.Tx }

func NewSettlements(tx pgx.Tx) *Settlements { return &Settlements{tx: tx} }

func (r *Settlements) existing(ctx context.Context, q application.SettlementRequest) (*application.SettlementReceipt, error) {
	var v application.SettlementReceipt
	var outcome string
	err := r.tx.QueryRow(ctx, `SELECT workspace_id,run_id,reservation_id,price_version_id,budget_id,period_id,currency,reserved_micro,charged_micro,outcome,observed_at,settled_at FROM commerce.usage_settlements WHERE workspace_id=$1 AND run_id=$2`, q.WorkspaceID, q.RunID).Scan(&v.WorkspaceID, &v.RunID, &v.ReservationID, &v.PriceVersionID, &v.BudgetID, &v.PeriodID, &v.Currency, &v.ReservedMicro, &v.ChargedMicro, &outcome, &v.ObservedAt, &v.SettledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, application.ErrSettlementUnavailable
	}
	v.Outcome = domain.SettlementOutcome(outcome)
	return &v, nil
}

func (r *Settlements) LockSettlement(ctx context.Context, q application.SettlementRequest) (application.LockedSettlement, error) {
	if r == nil || r.tx == nil {
		return application.LockedSettlement{}, application.ErrSettlementUnavailable
	}
	if old, err := r.existing(ctx, q); err != nil || old != nil {
		return application.LockedSettlement{Existing: old}, err
	}
	var locked application.LockedSettlement
	var snap domain.AllowanceSnapshot
	var currency string
	err := r.tx.QueryRow(ctx, `SELECT currency,limit_micro,consumed_micro,reserved_micro,revision FROM commerce.budget_periods WHERE workspace_id=$1 AND budget_id=$2 AND period_id=$3 FOR UPDATE`, q.WorkspaceID, q.BudgetID, q.PeriodID).Scan(&currency, &snap.Limit, &snap.Consumed, &snap.Reserved, &snap.Revision)
	if err != nil || currency != q.Currency {
		return application.LockedSettlement{}, application.ErrSettlementConflict
	}
	locked.AllowanceRevision = snap.Revision
	locked.Allowance, err = domain.RestoreAllowance(snap)
	if err != nil {
		return application.LockedSettlement{}, application.ErrSettlementUnavailable
	}
	var releasedAt, settledAt *time.Time
	var charged *int64
	err = r.tx.QueryRow(ctx, `SELECT state,amount_micro,created_at,released_at,settled_at,charged_micro FROM commerce.reservations WHERE workspace_id=$1 AND id=$2 AND run_id=$3 AND budget_id=$4 AND period_id=$5 AND currency=$6 FOR UPDATE`, q.WorkspaceID, q.ReservationID, q.RunID, q.BudgetID, q.PeriodID, q.Currency).Scan(&locked.ReservationState, &locked.ReservationAmount, &locked.ReservationAt, &releasedAt, &settledAt, &charged)
	if err != nil {
		return application.LockedSettlement{}, application.ErrSettlementConflict
	}
	err = r.tx.QueryRow(ctx, `SELECT id,tool_version_id,currency,reserve_micro,charge_micro,billing_policy,starts_at,ends_at,active FROM commerce.price_versions WHERE id=$1`, q.PriceVersionID).Scan(&locked.Price.ID, &locked.Price.ToolVersionID, &locked.Price.Currency, &locked.Price.ReserveMicro, &locked.Price.ChargeMicro, &locked.Price.BillingPolicy, &locked.Price.StartsAt, &locked.Price.EndsAt, &locked.Price.Active)
	if err != nil || !locked.Price.ValidContract() {
		return application.LockedSettlement{}, application.ErrSettlementConflict
	}
	return locked, nil
}

func (r *Settlements) StoreSettlement(ctx context.Context, q application.SettlementRequest, allowance domain.Allowance, expectedRevision, charged int64) (application.SettlementReceipt, error) {
	state := allowance.Snapshot()
	tag, err := r.tx.Exec(ctx, `UPDATE commerce.budget_periods SET consumed_micro=$1,reserved_micro=$2,revision=$3 WHERE workspace_id=$4 AND budget_id=$5 AND period_id=$6 AND revision=$7`, state.Consumed, state.Reserved, state.Revision, q.WorkspaceID, q.BudgetID, q.PeriodID, expectedRevision)
	if err != nil || tag.RowsAffected() != 1 {
		return application.SettlementReceipt{}, application.ErrSettlementUnavailable
	}
	tag, err = r.tx.Exec(ctx, `UPDATE commerce.reservations SET state='settled',charged_micro=$1,settled_at=$2 WHERE workspace_id=$3 AND id=$4 AND run_id=$5 AND state='held' AND amount_micro=$6`, charged, q.SettledAt, q.WorkspaceID, q.ReservationID, q.RunID, q.ReservedMicro)
	if err != nil || tag.RowsAffected() != 1 {
		return application.SettlementReceipt{}, application.ErrSettlementConflict
	}
	_, err = r.tx.Exec(ctx, `INSERT INTO commerce.usage_settlements(workspace_id,run_id,reservation_id,price_version_id,budget_id,period_id,currency,reserved_micro,charged_micro,outcome,observed_at,settled_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, q.WorkspaceID, q.RunID, q.ReservationID, q.PriceVersionID, q.BudgetID, q.PeriodID, q.Currency, q.ReservedMicro, charged, string(q.Outcome), q.ObservedAt, q.SettledAt)
	if err != nil {
		return application.SettlementReceipt{}, application.ErrSettlementUnavailable
	}
	return application.SettlementReceipt{WorkspaceID: q.WorkspaceID, RunID: q.RunID, ReservationID: q.ReservationID, PriceVersionID: q.PriceVersionID, BudgetID: q.BudgetID, PeriodID: q.PeriodID, Currency: q.Currency, ReservedMicro: q.ReservedMicro, ChargedMicro: charged, Outcome: q.Outcome, ObservedAt: q.ObservedAt, SettledAt: q.SettledAt}, nil
}

var _ application.SettlementRepository = (*Settlements)(nil)
