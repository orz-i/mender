package domain

import (
	"testing"
	"time"
)

func TestLedgerJournalRequiresExactBalancedShape(t *testing.T) {
	at := time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)
	charge := Journal{
		ID: "journal_charge", WorkspaceID: "ws_a", BusinessKey: "charge:run_a", Kind: JournalCharge, Currency: "USD", AmountMicro: 70,
		BasisKind: BasisUsageSettlement, BasisID: "run_a", Reason: "usage settlement", OccurredAt: at,
		Entries: []LedgerEntry{{Account: AccountWorkspaceReceivable, Currency: "USD", DeltaMicro: 70}, {Account: AccountPlatformRevenue, Currency: "USD", DeltaMicro: -70}},
	}
	if !charge.Valid() {
		t.Fatal("valid charge journal rejected")
	}
	broken := charge
	broken.Entries = append([]LedgerEntry(nil), charge.Entries...)
	broken.Entries[1].DeltaMicro = -69
	if broken.Valid() {
		t.Fatal("unbalanced journal accepted")
	}

	refund := Journal{
		ID: "journal_refund", WorkspaceID: "ws_a", BusinessKey: "refund:ticket_1", Kind: JournalRefund, Direction: AdjustmentCredit, Currency: "USD", AmountMicro: 20,
		BasisKind: BasisRun, BasisID: "run_a", ApprovalID: "approval_refund", ActorUserID: "finance_operator", Reason: "customer credit", OccurredAt: at,
		Entries: []LedgerEntry{{Account: AccountWorkspaceReceivable, Currency: "USD", DeltaMicro: -20}, {Account: AccountPlatformRefund, Currency: "USD", DeltaMicro: 20}},
	}
	if !refund.Valid() {
		t.Fatal("valid refund journal rejected")
	}
	refund.ApprovalID = ""
	if refund.Valid() {
		t.Fatal("refund without approval accepted")
	}
}

func TestAdjustmentDirectionIsBoundIntoJournalShape(t *testing.T) {
	at := time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)
	journal := Journal{
		ID: "journal_adjust", WorkspaceID: "ws_a", BusinessKey: "adjust:case_1", Kind: JournalAdjustment, Direction: AdjustmentDebit, Currency: "USD", AmountMicro: 11,
		BasisKind: BasisIncident, BasisID: "incident_1", ApprovalID: "approval_adjust", ActorUserID: "finance_operator", Reason: "late provider cost correction", OccurredAt: at,
		Entries: []LedgerEntry{{Account: AccountWorkspaceReceivable, Currency: "USD", DeltaMicro: 11}, {Account: AccountPlatformAdjustment, Currency: "USD", DeltaMicro: -11}},
	}
	if !journal.Valid() {
		t.Fatal("valid debit adjustment rejected")
	}
	journal.Direction = AdjustmentCredit
	if journal.Valid() {
		t.Fatal("direction drift did not invalidate entries")
	}
}

func TestBillingAndReconciliationSummariesRebuildFromFacts(t *testing.T) {
	at := time.Date(2026, 9, 16, 1, 0, 0, 0, time.UTC)
	billing := BillingSummary{WorkspaceID: "ws_a", Currency: "USD", ChargedMicro: 100, RefundedMicro: 20, AdjustmentDebitMicro: 7, AdjustmentCreditMicro: 2, NetBilledMicro: 85, JournalCount: 4, LatestJournalAt: at}
	if !billing.Valid() {
		t.Fatal("valid billing summary rejected")
	}
	billing.NetBilledMicro++
	if billing.Valid() {
		t.Fatal("billing summary accepted unrebuildable total")
	}

	reconcile := ReconciliationSummary{WorkspaceID: "ws_a", Currency: "USD", UsageSettlementChargedMicro: 100, LedgerChargeMicro: 90, MissingChargeJournalCount: 1, PendingReconcileCount: 2, DifferenceMicro: -10}
	if !reconcile.Valid() {
		t.Fatal("valid reconciliation summary rejected")
	}
	reconcile.DifferenceMicro = 0
	if reconcile.Valid() {
		t.Fatal("reconciliation accepted inconsistent difference")
	}
}
