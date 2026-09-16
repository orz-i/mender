package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/domain"
)

type BillingRepository struct{ pool *pgxpool.Pool }

func NewBilling(pool *pgxpool.Pool) *BillingRepository { return &BillingRepository{pool: pool} }

func billingError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return application.ErrBillingNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "22023":
			return application.ErrBillingInvalid
		case "23505", "23514", "40001":
			return application.ErrBillingConflict
		case "42501":
			return application.ErrBillingForbidden
		case "P0002":
			return application.ErrBillingNotFound
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return application.ErrBillingUnavailable
}

func (r *BillingRepository) begin(ctx context.Context, workspace string) (pgx.Tx, error) {
	if r == nil || r.pool == nil {
		return nil, application.ErrBillingUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrBillingUnavailable
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		_ = tx.Rollback(ctx)
		return nil, application.ErrBillingUnavailable
	}
	return tx, nil
}

func (r *BillingRepository) BillingSummary(ctx context.Context, workspace, actor, currency string) (domain.BillingSummary, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return domain.BillingSummary{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var value domain.BillingSummary
	var latest *time.Time
	err = tx.QueryRow(ctx, `SELECT * FROM commerce.billing_summary($1,$2,$3)`, workspace, actor, currency).Scan(&value.WorkspaceID, &value.Currency, &value.ChargedMicro, &value.RefundedMicro, &value.AdjustmentDebitMicro, &value.AdjustmentCreditMicro, &value.NetBilledMicro, &value.JournalCount, &latest)
	if err != nil {
		return domain.BillingSummary{}, billingError(err)
	}
	if latest != nil {
		value.LatestJournalAt = *latest
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.BillingSummary{}, application.ErrBillingUnavailable
	}
	return value, nil
}

func (r *BillingRepository) BillingReconciliation(ctx context.Context, workspace, actor, currency string) (domain.ReconciliationSummary, error) {
	tx, err := r.begin(ctx, workspace)
	if err != nil {
		return domain.ReconciliationSummary{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var value domain.ReconciliationSummary
	err = tx.QueryRow(ctx, `SELECT * FROM commerce.billing_reconciliation($1,$2,$3)`, workspace, actor, currency).Scan(&value.WorkspaceID, &value.Currency, &value.UsageSettlementChargedMicro, &value.LedgerChargeMicro, &value.MissingChargeJournalCount, &value.PendingReconcileCount, &value.DifferenceMicro)
	if err != nil {
		return domain.ReconciliationSummary{}, billingError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.ReconciliationSummary{}, application.ErrBillingUnavailable
	}
	return value, nil
}

func (r *BillingRepository) PostRefund(ctx context.Context, request application.RefundRequest, actor string, at time.Time) (application.BillingReceipt, error) {
	tx, err := r.begin(ctx, request.WorkspaceID)
	if err != nil {
		return application.BillingReceipt{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var receipt application.BillingReceipt
	err = tx.QueryRow(ctx, `SELECT * FROM commerce.post_billing_refund($1,$2,$3,$4,$5,$6,$7,$8,$9)`, request.WorkspaceID, request.BusinessKey, request.RunID, request.AmountMicro, request.Currency, request.ApprovalID, actor, request.Reason, at).Scan(&receipt.JournalID, &receipt.Replay)
	if err != nil {
		return application.BillingReceipt{}, billingError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return application.BillingReceipt{}, application.ErrBillingUnavailable
	}
	return receipt, nil
}

func (r *BillingRepository) PostAdjustment(ctx context.Context, request application.AdjustmentRequest, actor string, at time.Time) (application.BillingReceipt, error) {
	tx, err := r.begin(ctx, request.WorkspaceID)
	if err != nil {
		return application.BillingReceipt{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var receipt application.BillingReceipt
	err = tx.QueryRow(ctx, `SELECT * FROM commerce.post_billing_adjustment($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, request.WorkspaceID, request.BusinessKey, request.BasisKind, request.BasisID, request.Direction, request.AmountMicro, request.Currency, request.ApprovalID, actor, request.Reason, at).Scan(&receipt.JournalID, &receipt.Replay)
	if err != nil {
		return application.BillingReceipt{}, billingError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return application.BillingReceipt{}, application.ErrBillingUnavailable
	}
	return receipt, nil
}

var _ application.BillingRepository = (*BillingRepository)(nil)
