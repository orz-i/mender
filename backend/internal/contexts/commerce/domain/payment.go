package domain

import "time"

type PaymentMode string

const (
	PaymentModeSandbox PaymentMode = "sandbox"
)

type PaymentPurpose string

const (
	PaymentCollectCharge PaymentPurpose = "collect_charge"
	PaymentExecuteRefund PaymentPurpose = "execute_refund"
)

type PaymentIntentState string

const (
	PaymentIntentPending PaymentIntentState = "pending"
	PaymentIntentSettled PaymentIntentState = "settled"
	PaymentIntentFailed  PaymentIntentState = "failed"
)

type PaymentIntent struct {
	ID, WorkspaceID, BusinessKey               string
	ProviderID, ProviderAccountID              string
	Mode                                       PaymentMode
	Purpose                                    PaymentPurpose
	BillingJournalID, Currency                 string
	AmountMicro                                int64
	State                                      PaymentIntentState
	Revision                                   int64
	CreatedAt, UpdatedAt, SettledAt, FailedAt  time.Time
	ProviderTransactionID, LastProviderEventID string
}

func validPaymentHandle(value string, max int) bool {
	if len(value) < 1 || len(value) > max {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '.' || ch == ':') {
			return false
		}
	}
	return true
}

func (p PaymentIntent) Valid() bool {
	if !validID(p.ID) || !validID(p.WorkspaceID) || !validBusinessKey(p.BusinessKey) || !validID(p.ProviderID) ||
		!validPaymentHandle(p.ProviderAccountID, 128) || p.Mode != PaymentModeSandbox || !validID(p.BillingJournalID) ||
		!validCurrency(p.Currency) || p.AmountMicro <= 0 || p.Revision < 1 || p.CreatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) {
		return false
	}
	switch p.Purpose {
	case PaymentCollectCharge, PaymentExecuteRefund:
	default:
		return false
	}
	switch p.State {
	case PaymentIntentPending:
		return p.SettledAt.IsZero() && p.FailedAt.IsZero() && p.ProviderTransactionID == "" && p.LastProviderEventID == ""
	case PaymentIntentSettled:
		return !p.SettledAt.Before(p.CreatedAt) && p.FailedAt.IsZero() && validPaymentHandle(p.ProviderTransactionID, 200) && validPaymentHandle(p.LastProviderEventID, 200)
	case PaymentIntentFailed:
		return !p.FailedAt.Before(p.CreatedAt) && p.SettledAt.IsZero() && p.ProviderTransactionID == "" && validPaymentHandle(p.LastProviderEventID, 200)
	default:
		return false
	}
}

type PaymentEventState string

const (
	PaymentEventSucceeded PaymentEventState = "succeeded"
	PaymentEventFailed    PaymentEventState = "failed"
)

type PaymentEventDisposition string

const (
	PaymentEventAccepted    PaymentEventDisposition = "accepted"
	PaymentEventQuarantined PaymentEventDisposition = "quarantined"
	PaymentEventDuplicate   PaymentEventDisposition = "duplicate"
)

type PaymentEvent struct {
	ReceiptID, ProviderID, ProviderAccountID, EventID string
	EventType, IntentID, WorkspaceID                  string
	ProviderTransactionID, Currency                   string
	AmountMicro                                       int64
	State                                             PaymentEventState
	BodySHA256, KeyID                                 string
	Disposition                                       PaymentEventDisposition
	ReasonCode                                        string
	SignedAt, OccurredAt, ReceivedAt, ProcessedAt     time.Time
}

func (e PaymentEvent) Valid() bool {
	if len(e.ReceiptID) != 64 || !validPaymentHandle(e.ProviderID, 128) || !validPaymentHandle(e.ProviderAccountID, 128) ||
		!validPaymentHandle(e.EventID, 200) || !validPaymentHandle(e.EventType, 128) || !validID(e.IntentID) || !validID(e.WorkspaceID) ||
		!validCurrency(e.Currency) || e.AmountMicro <= 0 || len(e.BodySHA256) != 64 || !validID(e.KeyID) ||
		e.SignedAt.IsZero() || e.OccurredAt.IsZero() || e.ReceivedAt.IsZero() || e.ProcessedAt.IsZero() ||
		e.ReceivedAt.Before(e.SignedAt) || e.ProcessedAt.Before(e.ReceivedAt) {
		return false
	}
	for _, digest := range []string{e.ReceiptID, e.BodySHA256} {
		for _, ch := range digest {
			if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
				return false
			}
		}
	}
	switch e.State {
	case PaymentEventSucceeded:
		if !validPaymentHandle(e.ProviderTransactionID, 200) {
			return false
		}
	case PaymentEventFailed:
		if e.ProviderTransactionID != "" {
			return false
		}
	default:
		return false
	}
	switch e.Disposition {
	case PaymentEventAccepted, PaymentEventDuplicate:
		return e.ReasonCode == ""
	case PaymentEventQuarantined:
		return validPaymentHandle(e.ReasonCode, 128)
	default:
		return false
	}
}

type PaymentReconciliationSummary struct {
	WorkspaceID, ProviderID, ProviderAccountID, Currency string
	ExpectedCollectionMicro, SettledCollectionMicro      int64
	ExpectedRefundMicro, SettledRefundMicro              int64
	PendingIntentCount, QuarantinedEventCount            int64
	CollectionDifferenceMicro, RefundDifferenceMicro     int64
}

func (s PaymentReconciliationSummary) Valid() bool {
	return validID(s.WorkspaceID) && validID(s.ProviderID) && validPaymentHandle(s.ProviderAccountID, 128) && validCurrency(s.Currency) &&
		s.ExpectedCollectionMicro >= 0 && s.SettledCollectionMicro >= 0 && s.ExpectedRefundMicro >= 0 && s.SettledRefundMicro >= 0 &&
		s.PendingIntentCount >= 0 && s.QuarantinedEventCount >= 0 &&
		s.CollectionDifferenceMicro == s.SettledCollectionMicro-s.ExpectedCollectionMicro &&
		s.RefundDifferenceMicro == s.SettledRefundMicro-s.ExpectedRefundMicro
}
