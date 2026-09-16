package domain

import "time"

// JournalKind distinguishes immutable billing facts. Corrections are always
// supplemental journals; prior charge/refund/adjustment journals are never
// rewritten to change financial meaning.
type JournalKind string

const (
	JournalCharge     JournalKind = "charge"
	JournalRefund     JournalKind = "refund"
	JournalAdjustment JournalKind = "adjustment"
)

type AdjustmentDirection string

const (
	AdjustmentDebit  AdjustmentDirection = "debit"
	AdjustmentCredit AdjustmentDirection = "credit"
)

type LedgerAccount string

const (
	AccountWorkspaceReceivable LedgerAccount = "workspace_receivable"
	AccountPlatformRevenue     LedgerAccount = "platform_revenue"
	AccountPlatformRefund      LedgerAccount = "platform_refund"
	AccountPlatformAdjustment  LedgerAccount = "platform_adjustment"
)

type BusinessBasisKind string

const (
	BasisUsageSettlement BusinessBasisKind = "usage_settlement"
	BasisRun             BusinessBasisKind = "run"
	BasisIncident        BusinessBasisKind = "incident"
	BasisReconciliation  BusinessBasisKind = "reconciliation"
)

type LedgerEntry struct {
	Account    LedgerAccount
	Currency   string
	DeltaMicro int64
}

type Journal struct {
	ID, WorkspaceID, BusinessKey string
	Kind                         JournalKind
	Direction                    AdjustmentDirection
	Currency                     string
	AmountMicro                  int64
	BasisKind                    BusinessBasisKind
	BasisID                      string
	ApprovalID                   string
	ActorUserID                  string
	Reason                       string
	OccurredAt                   time.Time
	Entries                      []LedgerEntry
}

func validReason(value string) bool {
	runes := []rune(value)
	return len(runes) >= 1 && len(runes) <= 1000
}

func validBusinessKey(value string) bool {
	if len(value) < 1 || len(value) > 200 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == ':') {
			return false
		}
	}
	return true
}

func validBasis(kind BusinessBasisKind, id string) bool {
	if !validID(id) {
		return false
	}
	switch kind {
	case BasisUsageSettlement, BasisRun, BasisIncident, BasisReconciliation:
		return true
	default:
		return false
	}
}

func (j Journal) expectedEntries() (LedgerEntry, LedgerEntry, bool) {
	if j.AmountMicro < 0 || !validCurrency(j.Currency) {
		return LedgerEntry{}, LedgerEntry{}, false
	}
	switch j.Kind {
	case JournalCharge:
		if j.Direction != "" || j.BasisKind != BasisUsageSettlement || j.ApprovalID != "" || j.ActorUserID != "" || j.Reason != "usage settlement" {
			return LedgerEntry{}, LedgerEntry{}, false
		}
		return LedgerEntry{Account: AccountWorkspaceReceivable, Currency: j.Currency, DeltaMicro: j.AmountMicro}, LedgerEntry{Account: AccountPlatformRevenue, Currency: j.Currency, DeltaMicro: -j.AmountMicro}, true
	case JournalRefund:
		if j.Direction != AdjustmentCredit || !validID(j.ApprovalID) || !validID(j.ActorUserID) || !validReason(j.Reason) {
			return LedgerEntry{}, LedgerEntry{}, false
		}
		return LedgerEntry{Account: AccountWorkspaceReceivable, Currency: j.Currency, DeltaMicro: -j.AmountMicro}, LedgerEntry{Account: AccountPlatformRefund, Currency: j.Currency, DeltaMicro: j.AmountMicro}, true
	case JournalAdjustment:
		if !validID(j.ApprovalID) || !validID(j.ActorUserID) || !validReason(j.Reason) {
			return LedgerEntry{}, LedgerEntry{}, false
		}
		switch j.Direction {
		case AdjustmentDebit:
			return LedgerEntry{Account: AccountWorkspaceReceivable, Currency: j.Currency, DeltaMicro: j.AmountMicro}, LedgerEntry{Account: AccountPlatformAdjustment, Currency: j.Currency, DeltaMicro: -j.AmountMicro}, true
		case AdjustmentCredit:
			return LedgerEntry{Account: AccountWorkspaceReceivable, Currency: j.Currency, DeltaMicro: -j.AmountMicro}, LedgerEntry{Account: AccountPlatformAdjustment, Currency: j.Currency, DeltaMicro: j.AmountMicro}, true
		default:
			return LedgerEntry{}, LedgerEntry{}, false
		}
	default:
		return LedgerEntry{}, LedgerEntry{}, false
	}
}

func (j Journal) Valid() bool {
	if !validID(j.ID) || !validID(j.WorkspaceID) || !validBusinessKey(j.BusinessKey) || !validCurrency(j.Currency) || !validBasis(j.BasisKind, j.BasisID) || j.AmountMicro < 0 || j.OccurredAt.IsZero() || j.OccurredAt.Year() < 1 || j.OccurredAt.Year() > 9999 || len(j.Entries) != 2 {
		return false
	}
	first, second, ok := j.expectedEntries()
	if !ok {
		return false
	}
	return j.Entries[0] == first && j.Entries[1] == second && j.Entries[0].DeltaMicro+j.Entries[1].DeltaMicro == 0
}

type BillingSummary struct {
	WorkspaceID                 string
	Currency                    string
	ChargedMicro, RefundedMicro int64
	AdjustmentDebitMicro        int64
	AdjustmentCreditMicro       int64
	NetBilledMicro              int64
	JournalCount                int64
	LatestJournalAt             time.Time
}

func (s BillingSummary) Valid() bool {
	if !validID(s.WorkspaceID) || !validCurrency(s.Currency) || s.ChargedMicro < 0 || s.RefundedMicro < 0 || s.AdjustmentDebitMicro < 0 || s.AdjustmentCreditMicro < 0 || s.JournalCount < 0 {
		return false
	}
	if s.NetBilledMicro != s.ChargedMicro+s.AdjustmentDebitMicro-s.RefundedMicro-s.AdjustmentCreditMicro {
		return false
	}
	return s.JournalCount == 0 || !s.LatestJournalAt.IsZero()
}

type ReconciliationSummary struct {
	WorkspaceID, Currency                          string
	UsageSettlementChargedMicro, LedgerChargeMicro int64
	MissingChargeJournalCount                      int64
	PendingReconcileCount                          int64
	DifferenceMicro                                int64
}

func (s ReconciliationSummary) Valid() bool {
	return validID(s.WorkspaceID) && validCurrency(s.Currency) && s.UsageSettlementChargedMicro >= 0 && s.LedgerChargeMicro >= 0 && s.MissingChargeJournalCount >= 0 && s.PendingReconcileCount >= 0 && s.DifferenceMicro == s.LedgerChargeMicro-s.UsageSettlementChargedMicro
}
