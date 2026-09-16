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

type PaymentRepository struct{ pool *pgxpool.Pool }

func NewPayments(pool *pgxpool.Pool) *PaymentRepository { return &PaymentRepository{pool: pool} }

func paymentAdminError(err error) error {
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

func paymentTx(ctx context.Context, pool *pgxpool.Pool, workspace string) (pgx.Tx, error) {
	if pool == nil || workspace == "" {
		return nil, application.ErrBillingUnavailable
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, application.ErrBillingUnavailable
	}
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, workspace); err != nil {
		_ = tx.Rollback(context.Background())
		return nil, application.ErrBillingUnavailable
	}
	return tx, nil
}

func (r *PaymentRepository) CreateSandboxPaymentIntent(ctx context.Context, request application.PaymentIntentRequest, id, actor string, at time.Time) (application.PaymentIntentReceipt, error) {
	tx, err := paymentTx(ctx, r.pool, request.WorkspaceID)
	if err != nil {
		return application.PaymentIntentReceipt{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var receipt application.PaymentIntentReceipt
	err = tx.QueryRow(ctx, `SELECT * FROM commerce.create_sandbox_payment_intent($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		request.WorkspaceID, id, request.BusinessKey, request.ProviderID, request.ProviderAccountID, request.Mode, request.Purpose,
		request.BillingJournalID, request.Currency, request.AmountMicro, actor, at).Scan(&receipt.IntentID, &receipt.Replay)
	if err != nil {
		return application.PaymentIntentReceipt{}, paymentAdminError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return application.PaymentIntentReceipt{}, application.ErrBillingUnavailable
	}
	return receipt, nil
}

func (r *PaymentRepository) ListPaymentIntents(ctx context.Context, workspace, actor, provider, account string, limit int) ([]application.PaymentIntentView, error) {
	tx, err := paymentTx(ctx, r.pool, workspace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	rows, err := tx.Query(ctx, `SELECT * FROM commerce.payment_intent_projection($1,$2,$3,$4,$5)`, workspace, actor, provider, account, limit)
	if err != nil {
		return nil, paymentAdminError(err)
	}
	defer rows.Close()
	items := make([]application.PaymentIntentView, 0)
	for rows.Next() {
		var item application.PaymentIntentView
		var settledAt, failedAt *time.Time
		if err = rows.Scan(&item.ID, &item.BusinessKey, &item.Purpose, &item.BillingJournalID, &item.Currency, &item.AmountMicro, &item.State, &item.Revision, &item.CreatedAt, &item.UpdatedAt, &settledAt, &failedAt); err != nil {
			return nil, application.ErrBillingUnavailable
		}
		if settledAt != nil {
			item.SettledAt = *settledAt
		}
		if failedAt != nil {
			item.FailedAt = *failedAt
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, application.ErrBillingUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, application.ErrBillingUnavailable
	}
	return items, nil
}

func (r *PaymentRepository) ListPaymentCallbacks(ctx context.Context, workspace, actor, provider, account string, limit int) ([]application.PaymentCallbackView, error) {
	tx, err := paymentTx(ctx, r.pool, workspace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	rows, err := tx.Query(ctx, `SELECT * FROM commerce.payment_callback_projection($1,$2,$3,$4,$5)`, workspace, actor, provider, account, limit)
	if err != nil {
		return nil, paymentAdminError(err)
	}
	defer rows.Close()
	items := make([]application.PaymentCallbackView, 0)
	for rows.Next() {
		var item application.PaymentCallbackView
		var reason *string
		if err = rows.Scan(&item.ReceiptID, &item.EventID, &item.IntentID, &item.EventType, &item.Currency, &item.AmountMicro, &item.EventState, &item.OccurredAt, &item.Disposition, &reason, &item.ReceivedAt, &item.DeliveryCount); err != nil {
			return nil, application.ErrBillingUnavailable
		}
		if reason != nil {
			item.ReasonCode = *reason
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, application.ErrBillingUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, application.ErrBillingUnavailable
	}
	return items, nil
}

func (r *PaymentRepository) PaymentReconciliation(ctx context.Context, workspace, actor, provider, account, currency string) (domain.PaymentReconciliationSummary, error) {
	tx, err := paymentTx(ctx, r.pool, workspace)
	if err != nil {
		return domain.PaymentReconciliationSummary{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var value domain.PaymentReconciliationSummary
	err = tx.QueryRow(ctx, `SELECT * FROM commerce.payment_reconciliation($1,$2,$3,$4,$5)`, workspace, actor, provider, account, currency).Scan(
		&value.WorkspaceID, &value.ProviderID, &value.ProviderAccountID, &value.Currency,
		&value.ExpectedCollectionMicro, &value.SettledCollectionMicro, &value.ExpectedRefundMicro, &value.SettledRefundMicro,
		&value.PendingIntentCount, &value.QuarantinedEventCount, &value.CollectionDifferenceMicro, &value.RefundDifferenceMicro)
	if err != nil {
		return domain.PaymentReconciliationSummary{}, paymentAdminError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.PaymentReconciliationSummary{}, application.ErrBillingUnavailable
	}
	return value, nil
}

type PaymentCallbackRepository struct{ pool *pgxpool.Pool }

func NewPaymentCallbacks(pool *pgxpool.Pool) *PaymentCallbackRepository {
	return &PaymentCallbackRepository{pool: pool}
}

func (r *PaymentCallbackRepository) IngestPaymentCallback(ctx context.Context, callback application.VerifiedPaymentCallback) (application.PaymentCallbackReceipt, error) {
	if r == nil || r.pool == nil {
		return application.PaymentCallbackReceipt{}, application.ErrPaymentCallbackUnavailable
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return application.PaymentCallbackReceipt{}, application.ErrPaymentCallbackUnavailable
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT set_config('mender.workspace_id',$1,true)`, callback.WorkspaceID); err != nil {
		return application.PaymentCallbackReceipt{}, application.ErrPaymentCallbackUnavailable
	}
	var receipt application.PaymentCallbackReceipt
	var reason *string
	err = tx.QueryRow(ctx, `SELECT * FROM commerce.ingest_sandbox_payment_callback($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		callback.ProviderID, callback.ProviderAccountID, callback.EventID, callback.BodySHA256, callback.KeyID, callback.SignedAt, callback.ReceivedAt,
		callback.WorkspaceID, callback.IntentID, callback.EventType, nullablePayment(callback.ProviderTransactionID), callback.Currency, callback.AmountMicro,
		callback.EventState, callback.OccurredAt).Scan(&receipt.ReceiptID, &receipt.Disposition, &reason)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return application.PaymentCallbackReceipt{}, err
		}
		return application.PaymentCallbackReceipt{}, application.ErrPaymentCallbackUnavailable
	}
	receipt.EventID = callback.EventID
	if reason != nil {
		receipt.ReasonCode = *reason
	}
	if err = tx.Commit(ctx); err != nil {
		return application.PaymentCallbackReceipt{}, application.ErrPaymentCallbackUnavailable
	}
	return receipt, nil
}

func nullablePayment(value string) any {
	if value == "" {
		return nil
	}
	return value
}

var _ application.PaymentAdminRepository = (*PaymentRepository)(nil)
var _ application.PaymentCallbackReceiver = (*PaymentCallbackRepository)(nil)
