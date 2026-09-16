package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/domain"
)

type paymentClock struct{ at time.Time }

func (c paymentClock) Now() time.Time { return c.at }

type paymentAuth struct{ action string }

func (*paymentAuth) AuthenticateBilling(context.Context, string) (BillingActor, error) {
	return BillingActor{UserID: "finance_operator"}, nil
}
func (*paymentAuth) AuthenticateBillingMutation(context.Context, string, string) (BillingActor, error) {
	return BillingActor{UserID: "finance_operator"}, nil
}
func (a *paymentAuth) AuthorizeBillingPlatform(_ context.Context, _ BillingActor, action string) error {
	a.action = action
	return nil
}

type paymentIDs struct{}

func (paymentIDs) NewPaymentIntentID() (string, error) { return "pay_test", nil }

type paymentAdminRepo struct {
	createCalls int
	request     PaymentIntentRequest
	reconcile   domain.PaymentReconciliationSummary
}

func (r *paymentAdminRepo) CreateSandboxPaymentIntent(_ context.Context, req PaymentIntentRequest, id, _ string, _ time.Time) (PaymentIntentReceipt, error) {
	r.createCalls++
	r.request = req
	return PaymentIntentReceipt{IntentID: id}, nil
}
func (*paymentAdminRepo) ListPaymentIntents(context.Context, string, string, string, string, int) ([]PaymentIntentView, error) {
	return nil, nil
}
func (*paymentAdminRepo) ListPaymentCallbacks(context.Context, string, string, string, string, int) ([]PaymentCallbackView, error) {
	return nil, nil
}
func (r *paymentAdminRepo) PaymentReconciliation(context.Context, string, string, string, string, string) (domain.PaymentReconciliationSummary, error) {
	return r.reconcile, nil
}

func TestPaymentIntentRejectsLiveModeBeforeRepository(t *testing.T) {
	repo := &paymentAdminRepo{}
	auth := &paymentAuth{}
	svc, err := NewPaymentAdminService(repo, auth, paymentIDs{}, paymentClock{time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateIntent(context.Background(), BillingActor{UserID: "finance_operator"}, PaymentIntentRequest{
		WorkspaceID: "ws_a", BusinessKey: "payment:run_a", ProviderID: "sandbox_psp", ProviderAccountID: "sandbox_account",
		Mode: "live", Purpose: "collect_charge", BillingJournalID: "charge_a", Currency: "USD", AmountMicro: 100,
	})
	if !errors.Is(err, ErrBillingInvalid) || repo.createCalls != 0 {
		t.Fatal("live payment reached repository", err, repo.createCalls)
	}
}

func TestPaymentIntentPreservesExactBillingJournalBinding(t *testing.T) {
	repo := &paymentAdminRepo{}
	auth := &paymentAuth{}
	svc, _ := NewPaymentAdminService(repo, auth, paymentIDs{}, paymentClock{time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)})
	req := PaymentIntentRequest{WorkspaceID: "ws_a", BusinessKey: "payment:refund:run_a", ProviderID: "sandbox_psp", ProviderAccountID: "sandbox_account", Mode: "sandbox", Purpose: "execute_refund", BillingJournalID: "refund_a", Currency: "USD", AmountMicro: 25}
	receipt, err := svc.CreateIntent(context.Background(), BillingActor{UserID: "finance_operator"}, req)
	if err != nil || receipt.IntentID != "pay_test" || repo.createCalls != 1 || repo.request != req || auth.action != "platform:operate" {
		t.Fatal(receipt, repo, auth.action, err)
	}
}

type callbackVerifier struct {
	verification PaymentCallbackVerification
	err          error
	calls        int
}

func (v *callbackVerifier) VerifyPaymentCallback(context.Context, string, string, string, string, string, []byte, time.Time) (PaymentCallbackVerification, error) {
	v.calls++
	return v.verification, v.err
}

type callbackReceiver struct {
	calls    int
	callback VerifiedPaymentCallback
}

func (r *callbackReceiver) IngestPaymentCallback(_ context.Context, callback VerifiedPaymentCallback) (PaymentCallbackReceipt, error) {
	r.calls++
	r.callback = callback
	return PaymentCallbackReceipt{ReceiptID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", EventID: callback.EventID, Disposition: "accepted"}, nil
}

func TestPaymentCallbackVerifiesBeforeAnyBusinessMutation(t *testing.T) {
	receiver := &callbackReceiver{}
	verifier := &callbackVerifier{err: ErrPaymentCallbackUnauthorized}
	svc, _ := NewPaymentCallbackService(receiver, verifier, paymentClock{time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)})
	_, err := svc.Handle(context.Background(), "sandbox_psp", "sandbox_account", "key_a", "1789538400", "v1=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", []byte(`{"schema_version":1}`))
	if !errors.Is(err, ErrPaymentCallbackUnauthorized) || verifier.calls != 1 || receiver.calls != 0 {
		t.Fatal("unverified callback reached receiver", err, verifier.calls, receiver.calls)
	}
}

func TestPaymentCallbackCarriesExactAmountCurrencyIntentWithoutLedgerMutationPort(t *testing.T) {
	at := time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)
	receiver := &callbackReceiver{}
	verifier := &callbackVerifier{verification: PaymentCallbackVerification{SignedAt: at.Add(-time.Second), BodySHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
	svc, _ := NewPaymentCallbackService(receiver, verifier, paymentClock{at})
	body := []byte(`{"schema_version":1,"event_type":"refund.succeeded","event_id":"evt.1","workspace_id":"ws_a","intent_id":"pay_a","provider_transaction_id":"txn.1","currency":"USD","amount_micro":"25","occurred_at":"2026-09-16T05:59:59Z"}`)
	receipt, err := svc.Handle(context.Background(), "sandbox_psp", "sandbox_account", "key_a", "1789538399", "v1=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", body)
	if err != nil || receiver.calls != 1 || receipt.Disposition != "accepted" {
		t.Fatal(receipt, receiver.calls, err)
	}
	got := receiver.callback
	if got.IntentID != "pay_a" || got.WorkspaceID != "ws_a" || got.EventType != "refund.succeeded" || got.Currency != "USD" || got.AmountMicro != 25 || got.ProviderTransactionID != "txn.1" || got.EventState != "succeeded" {
		t.Fatal("callback binding drifted", got)
	}
}
