//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	commercepg "github.com/orz-i/mender/backend/internal/contexts/commerce/adapters/outbound/postgres"
	commerceapp "github.com/orz-i/mender/backend/internal/contexts/commerce/application"
	database "github.com/orz-i/mender/backend/internal/platform/postgres"
	"github.com/orz-i/mender/backend/migrations"
)

func createSandboxPaymentIntent(t *testing.T, ctx context.Context, manager *pgxpool.Pool, workspace, id, businessKey, purpose, journal string, amount int64, actor string, at time.Time) {
	t.Helper()
	tx := beginWorkspaceTx(t, ctx, manager, workspace)
	var got string
	var replay bool
	must(t, tx.QueryRow(ctx, `SELECT * FROM commerce.create_sandbox_payment_intent($1,$2,$3,'sandbox_psp','account.one','sandbox',$4,$5,'USD',$6,$7,$8)`,
		workspace, id, businessKey, purpose, journal, amount, actor, at).Scan(&got, &replay))
	must(t, tx.Commit(ctx))
	if got != id || replay {
		t.Fatal("payment intent creation drifted", got, replay)
	}
}

func paymentCallback(t *testing.T, ctx context.Context, repo *commercepg.PaymentCallbackRepository, workspace, intent, eventID, bodyDigest, eventType, providerTxn, currency string, amount int64, state string, at time.Time) commerceapp.PaymentCallbackReceipt {
	t.Helper()
	receipt, err := repo.IngestPaymentCallback(ctx, commerceapp.VerifiedPaymentCallback{
		ProviderID: "sandbox_psp", ProviderAccountID: "account.one", EventID: eventID, BodySHA256: bodyDigest, KeyID: "key_a",
		WorkspaceID: workspace, IntentID: intent, EventType: eventType, ProviderTransactionID: providerTxn, Currency: currency,
		AmountMicro: amount, EventState: state, SignedAt: at.Add(-time.Second), ReceivedAt: at.Add(time.Second), OccurredAt: at,
	})
	must(t, err)
	return receipt
}

func exerciseSandboxPaymentCallbacks(t *testing.T, ctx context.Context, owner *pgxpool.Pool, runtimeDSN string) {
	t.Helper()
	manager, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_payment_manager_", migrations.GrantPaymentManager)
	ingestor, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_payment_callback_", migrations.GrantPaymentCallbackIngestor)
	dangerous, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_payment_danger_", migrations.GrantDangerousOperationManager)
	billing, _ := openTemporaryRole(t, ctx, owner, runtimeDSN, "mender_payment_billing_", migrations.GrantBillingManager)
	must(t, database.PaymentManagerRole(ctx, manager))
	must(t, database.PaymentCallbackIngestorRole(ctx, ingestor))
	if database.PaymentManagerRole(ctx, owner) == nil || database.PaymentManagerRole(ctx, ingestor) == nil || database.PaymentCallbackIngestorRole(ctx, manager) == nil {
		t.Fatal("payment restricted role accepted a cross-purpose principal")
	}
	for _, probe := range []struct {
		pool *pgxpool.Pool
		sql  string
	}{
		{manager, `SELECT count(*) FROM commerce.payment_intents`},
		{manager, `SELECT count(*) FROM commerce.billing_journals`},
		{ingestor, `SELECT count(*) FROM commerce.payment_callback_inbox`},
		{ingestor, `SELECT count(*) FROM commerce.payment_intents`},
		{ingestor, `SELECT count(*) FROM identity.platform_staff`},
	} {
		if _, err := probe.pool.Exec(ctx, probe.sql); err == nil {
			t.Fatal("restricted payment role gained direct business table access", probe.sql)
		}
	}

	workspace := "ws_payment_t27"
	base := time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)
	_, err := owner.Exec(ctx, `INSERT INTO identity.workspaces(id,created_at) VALUES($1,$2)`, workspace, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.users(id,display_name,created_at) VALUES
	 ('payment_operator','Payment Operator',$1),('payment_reviewer','Payment Reviewer',$1),('payment_auditor','Payment Auditor',$1)`, base)
	must(t, err)
	_, err = owner.Exec(ctx, `INSERT INTO identity.platform_staff(user_id,role,created_at) VALUES
	 ('payment_operator','operator',$1),('payment_reviewer','reviewer',$1),('payment_auditor','auditor',$1)`, base)
	must(t, err)
	seedSettledBillingRun(t, ctx, owner, workspace, "run_payment_charge", "payment_charge", 100, 70, base.Add(time.Minute))
	var chargeJournal string
	must(t, owner.QueryRow(ctx, `SELECT id FROM commerce.billing_journals WHERE workspace_id=$1 AND business_key='usage:run_payment_charge'`, workspace).Scan(&chargeJournal))

	expectScopedPGCode(t, ctx, manager, workspace, "42501", `SELECT commerce.create_sandbox_payment_intent($1,'pay_live','payment:live','sandbox_psp','account.one','live','collect_charge',$2,'USD',70,'payment_operator',$3)`, workspace, chargeJournal, base.Add(4*time.Minute))
	expectScopedPGCode(t, ctx, manager, workspace, "23514", `SELECT commerce.create_sandbox_payment_intent($1,'pay_wrong_amount','payment:wrong-amount','sandbox_psp','account.one','sandbox','collect_charge',$2,'USD',71,'payment_operator',$3)`, workspace, chargeJournal, base.Add(4*time.Minute))

	createSandboxPaymentIntent(t, ctx, manager, workspace, "pay_charge", "payment:charge", "collect_charge", chargeJournal, 70, "payment_operator", base.Add(4*time.Minute))
	callbackRepo := commercepg.NewPaymentCallbacks(ingestor)
	receipt := paymentCallback(t, ctx, callbackRepo, workspace, "pay_charge", "evt.charge.1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "payment.succeeded", "txn.charge.1", "USD", 70, "succeeded", base.Add(5*time.Minute))
	if receipt.Disposition != "accepted" || receipt.ReasonCode != "" {
		t.Fatal("valid payment callback not accepted", receipt)
	}
	var state string
	var revision int64
	must(t, owner.QueryRow(ctx, `SELECT state,revision FROM commerce.payment_intents WHERE workspace_id=$1 AND id='pay_charge'`, workspace).Scan(&state, &revision))
	if state != "settled" || revision != 2 {
		t.Fatal("accepted payment did not settle exact intent", state, revision)
	}

	replay := paymentCallback(t, ctx, callbackRepo, workspace, "pay_charge", "evt.charge.1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "payment.succeeded", "txn.charge.1", "USD", 70, "succeeded", base.Add(5*time.Minute))
	if replay.Disposition != "duplicate" || replay.ReceiptID != receipt.ReceiptID {
		t.Fatal("exact payment replay was not idempotent", replay, receipt)
	}
	var deliveries int
	must(t, owner.QueryRow(ctx, `SELECT delivery_count FROM commerce.payment_callback_inbox WHERE receipt_id=$1`, receipt.ReceiptID).Scan(&deliveries))
	if deliveries != 2 {
		t.Fatal("duplicate callback did not increment delivery evidence exactly once", deliveries)
	}

	conflict := paymentCallback(t, ctx, callbackRepo, workspace, "pay_charge", "evt.charge.1", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "payment.succeeded", "txn.charge.1", "USD", 71, "succeeded", base.Add(6*time.Minute))
	if conflict.Disposition != "quarantined" || conflict.ReasonCode != "event_id_conflict" {
		t.Fatal("same event ID fact drift was not quarantined", conflict)
	}
	mismatch := paymentCallback(t, ctx, callbackRepo, workspace, "pay_charge", "evt.charge.currency", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "payment.succeeded", "txn.charge.currency", "EUR", 70, "succeeded", base.Add(6*time.Minute))
	if mismatch.Disposition != "quarantined" || mismatch.ReasonCode != "binding_mismatch" {
		t.Fatal("currency mismatch was not quarantined", mismatch)
	}
	missing := paymentCallback(t, ctx, callbackRepo, workspace, "pay_missing", "evt.missing", "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "payment.succeeded", "txn.missing", "USD", 70, "succeeded", base.Add(6*time.Minute))
	if missing.Disposition != "quarantined" || missing.ReasonCode != "intent_not_found" {
		t.Fatal("missing payment intent was not quarantined", missing)
	}
	late := paymentCallback(t, ctx, callbackRepo, workspace, "pay_charge", "evt.charge.late", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", "payment.succeeded", "txn.charge.late", "USD", 70, "succeeded", base.Add(7*time.Minute))
	if late.Disposition != "quarantined" || late.ReasonCode != "intent_already_terminal" {
		t.Fatal("late terminal callback was not quarantined", late)
	}

	createSandboxPaymentIntent(t, ctx, manager, workspace, "pay_failed", "payment:failed", "collect_charge", chargeJournal, 70, "payment_operator", base.Add(7*time.Minute))
	failed := paymentCallback(t, ctx, callbackRepo, workspace, "pay_failed", "evt.failed", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", "payment.failed", "", "USD", 70, "failed", base.Add(8*time.Minute))
	if failed.Disposition != "accepted" {
		t.Fatal("valid payment failure callback not accepted", failed)
	}
	must(t, owner.QueryRow(ctx, `SELECT state FROM commerce.payment_intents WHERE workspace_id=$1 AND id='pay_failed'`, workspace).Scan(&state))
	if state != "failed" {
		t.Fatal("payment failure did not terminally fail intent", state)
	}

	requestCommerceApproval(t, ctx, dangerous, workspace, "approval_payment_refund", "payment_operator", "commerce.refund", "refund:t27", "usage_settlement", "run_payment_charge", "credit", 20, "T27 external refund", base.Add(9*time.Minute))
	approveDanger(t, ctx, dangerous, workspace, "approval_payment_refund", "payment_reviewer", base.Add(10*time.Minute))
	tx := beginWorkspaceTx(t, ctx, billing, workspace)
	var refundJournal string
	var refundReplay bool
	must(t, tx.QueryRow(ctx, `SELECT * FROM commerce.post_billing_refund($1,'refund:t27','run_payment_charge',20,'USD','approval_payment_refund','payment_operator','T27 external refund',$2)`, workspace, base.Add(11*time.Minute)).Scan(&refundJournal, &refundReplay))
	must(t, tx.Commit(ctx))
	if refundReplay {
		t.Fatal("refund fixture unexpectedly replayed")
	}
	var beforeRows int
	var beforeSum int64
	must(t, owner.QueryRow(ctx, `SELECT count(*),coalesce(sum(delta_micro),0) FROM commerce.billing_entries WHERE workspace_id=$1 AND journal_id=$2`, workspace, refundJournal).Scan(&beforeRows, &beforeSum))
	createSandboxPaymentIntent(t, ctx, manager, workspace, "pay_refund", "payment:refund", "execute_refund", refundJournal, 20, "payment_operator", base.Add(12*time.Minute))
	refundReceipt := paymentCallback(t, ctx, callbackRepo, workspace, "pay_refund", "evt.refund.1", "1111111111111111111111111111111111111111111111111111111111111111", "refund.succeeded", "txn.refund.1", "USD", 20, "succeeded", base.Add(13*time.Minute))
	if refundReceipt.Disposition != "accepted" {
		t.Fatal("valid refund provider confirmation not accepted", refundReceipt)
	}
	var afterRows int
	var afterSum int64
	must(t, owner.QueryRow(ctx, `SELECT count(*),coalesce(sum(delta_micro),0) FROM commerce.billing_entries WHERE workspace_id=$1 AND journal_id=$2`, workspace, refundJournal).Scan(&afterRows, &afterSum))
	if beforeRows != 2 || afterRows != beforeRows || beforeSum != 0 || afterSum != beforeSum {
		t.Fatal("provider refund confirmation rewrote S4-03 refund journal", beforeRows, afterRows, beforeSum, afterSum)
	}

	adminRepo := commercepg.NewPayments(manager)
	summary, err := adminRepo.PaymentReconciliation(ctx, workspace, "payment_auditor", "sandbox_psp", "account.one", "USD")
	must(t, err)
	// Reconciliation is currency-scoped. The deliberately quarantined EUR
	// mismatch remains durable evidence but is not counted in this USD view.
	if summary.ExpectedCollectionMicro != 140 || summary.SettledCollectionMicro != 70 || summary.ExpectedRefundMicro != 20 || summary.SettledRefundMicro != 20 || summary.PendingIntentCount != 0 || summary.QuarantinedEventCount != 3 || summary.CollectionDifferenceMicro != -70 || summary.RefundDifferenceMicro != 0 {
		t.Fatal("payment reconciliation did not rebuild from immutable intent/event facts", summary)
	}
	callbacks, err := adminRepo.ListPaymentCallbacks(ctx, workspace, "payment_auditor", "sandbox_psp", "account.one", 100)
	must(t, err)
	if len(callbacks) < 7 {
		t.Fatal("safe payment callback projection omitted evidence", len(callbacks))
	}
	if _, err = manager.Exec(ctx, `SELECT body_sha256 FROM commerce.payment_callback_inbox LIMIT 1`); err == nil {
		t.Fatal("payment manager could read callback body digest directly")
	}

	if _, err = owner.Exec(ctx, `UPDATE commerce.payment_intents SET amount_micro=71 WHERE workspace_id=$1 AND id='pay_charge'`, workspace); pgErrorCode(err) != "42501" {
		t.Fatal("payment intent immutable binding was writable", err)
	}
	if _, err = owner.Exec(ctx, `DELETE FROM commerce.payment_callback_inbox WHERE receipt_id=$1`, receipt.ReceiptID); pgErrorCode(err) != "42501" {
		t.Fatal("payment callback evidence was deletable", err)
	}
}
