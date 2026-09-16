package domain

import (
	"testing"
	"time"
)

func TestPaymentIntentIsSandboxBoundAndExact(t *testing.T) {
	at := time.Date(2026, 9, 16, 5, 30, 0, 0, time.UTC)
	base := PaymentIntent{
		ID: "pay_a", WorkspaceID: "ws_a", BusinessKey: "payment:charge:run_a", ProviderID: "sandbox_psp", ProviderAccountID: "sandbox_account",
		Mode: PaymentModeSandbox, Purpose: PaymentCollectCharge, BillingJournalID: "charge_a", Currency: "USD", AmountMicro: 70000,
		State: PaymentIntentPending, Revision: 1, CreatedAt: at, UpdatedAt: at,
	}
	if !base.Valid() {
		t.Fatal("valid sandbox payment intent rejected")
	}
	for _, mutate := range []func(*PaymentIntent){
		func(v *PaymentIntent) { v.Mode = "live" },
		func(v *PaymentIntent) { v.AmountMicro = 0 },
		func(v *PaymentIntent) { v.Currency = "usd" },
		func(v *PaymentIntent) { v.BillingJournalID = "" },
		func(v *PaymentIntent) { v.State = PaymentIntentSettled },
	} {
		value := base
		mutate(&value)
		if value.Valid() {
			t.Fatal("invalid payment intent accepted", value)
		}
	}
}

func TestPaymentEventRequiresSafeVerifiedFacts(t *testing.T) {
	at := time.Date(2026, 9, 16, 5, 31, 0, 0, time.UTC)
	event := PaymentEvent{
		ReceiptID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProviderID: "sandbox_psp", ProviderAccountID: "sandbox_account",
		EventID: "evt.1", EventType: "payment.succeeded", IntentID: "pay_a", WorkspaceID: "ws_a", ProviderTransactionID: "txn.1",
		Currency: "USD", AmountMicro: 70000, State: PaymentEventSucceeded,
		BodySHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", KeyID: "key_a", Disposition: PaymentEventAccepted,
		SignedAt: at, OccurredAt: at, ReceivedAt: at.Add(time.Second), ProcessedAt: at.Add(time.Second),
	}
	if !event.Valid() {
		t.Fatal("valid payment event rejected")
	}
	for _, mutate := range []func(*PaymentEvent){
		func(v *PaymentEvent) { v.BodySHA256 = "not-a-digest" },
		func(v *PaymentEvent) { v.ProviderTransactionID = "" },
		func(v *PaymentEvent) { v.AmountMicro = -1 },
		func(v *PaymentEvent) { v.Disposition = PaymentEventQuarantined; v.ReasonCode = "" },
	} {
		value := event
		mutate(&value)
		if value.Valid() {
			t.Fatal("invalid payment event accepted", value)
		}
	}
}

func TestPaymentReconciliationDifferencesAreDerived(t *testing.T) {
	value := PaymentReconciliationSummary{
		WorkspaceID: "ws_a", ProviderID: "sandbox_psp", ProviderAccountID: "sandbox_account", Currency: "USD",
		ExpectedCollectionMicro: 100, SettledCollectionMicro: 70, ExpectedRefundMicro: 20, SettledRefundMicro: 5,
		PendingIntentCount: 2, QuarantinedEventCount: 1, CollectionDifferenceMicro: -30, RefundDifferenceMicro: -15,
	}
	if !value.Valid() {
		t.Fatal("valid reconciliation rejected")
	}
	value.CollectionDifferenceMicro++
	if value.Valid() {
		t.Fatal("hand-written reconciliation difference accepted")
	}
}
