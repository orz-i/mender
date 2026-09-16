package application

import (
	"context"
	"errors"
	"time"

	"github.com/orz-i/mender/backend/internal/contexts/commerce/domain"
)

var (
	ErrBillingUnavailable     = errors.New("billing unavailable")
	ErrBillingUnauthenticated = errors.New("billing unauthenticated")
	ErrBillingForbidden       = errors.New("billing forbidden")
	ErrBillingInvalid         = errors.New("billing invalid")
	ErrBillingNotFound        = errors.New("billing not found")
	ErrBillingConflict        = errors.New("billing conflict")
)

type BillingActor struct{ UserID string }

type BillingAuthorizer interface {
	AuthenticateBilling(context.Context, string) (BillingActor, error)
	AuthenticateBillingMutation(context.Context, string, string) (BillingActor, error)
	AuthorizeBillingPlatform(context.Context, BillingActor, string) error
}

type BillingReceipt struct {
	JournalID string
	Replay    bool
}

type RefundRequest struct {
	WorkspaceID, BusinessKey, RunID, Currency, ApprovalID, Reason string
	AmountMicro                                                   int64
}

type AdjustmentRequest struct {
	WorkspaceID, BusinessKey, BasisKind, BasisID, Direction, Currency, ApprovalID, Reason string
	AmountMicro                                                                           int64
}

type BillingRepository interface {
	BillingSummary(context.Context, string, string, string) (domain.BillingSummary, error)
	BillingReconciliation(context.Context, string, string, string) (domain.ReconciliationSummary, error)
	PostRefund(context.Context, RefundRequest, string, time.Time) (BillingReceipt, error)
	PostAdjustment(context.Context, AdjustmentRequest, string, time.Time) (BillingReceipt, error)
}

type BillingClock interface{ Now() time.Time }

type BillingService struct {
	repository BillingRepository
	auth       BillingAuthorizer
	clock      BillingClock
}

func NewBillingService(repository BillingRepository, auth BillingAuthorizer, clock BillingClock) (*BillingService, error) {
	if repository == nil || auth == nil || clock == nil {
		return nil, ErrBillingUnavailable
	}
	return &BillingService{repository: repository, auth: auth, clock: clock}, nil
}

func billingBusinessKey(value string) bool {
	if len(value) < 1 || len(value) > 200 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '.' || ch == ':') {
			return false
		}
	}
	return true
}

func billingReason(value string) bool { n := len([]rune(value)); return n >= 1 && n <= 1000 }

func (s *BillingService) now() (time.Time, error) {
	at := s.clock.Now().UTC().Truncate(time.Microsecond)
	if at.IsZero() || at.Year() > 9999 {
		return time.Time{}, ErrBillingUnavailable
	}
	return at, nil
}

func (s *BillingService) Summary(ctx context.Context, actor BillingActor, workspace, currency string) (domain.BillingSummary, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validCurrency(currency) {
		return domain.BillingSummary{}, ErrBillingInvalid
	}
	if err := s.auth.AuthorizeBillingPlatform(ctx, actor, "platform:audit"); err != nil {
		return domain.BillingSummary{}, err
	}
	value, err := s.repository.BillingSummary(ctx, workspace, actor.UserID, currency)
	if err != nil {
		return domain.BillingSummary{}, err
	}
	if value.WorkspaceID != workspace || value.Currency != currency || !value.Valid() {
		return domain.BillingSummary{}, ErrBillingUnavailable
	}
	return value, nil
}

func (s *BillingService) Reconciliation(ctx context.Context, actor BillingActor, workspace, currency string) (domain.ReconciliationSummary, error) {
	if !validID(actor.UserID) || !validID(workspace) || !validCurrency(currency) {
		return domain.ReconciliationSummary{}, ErrBillingInvalid
	}
	if err := s.auth.AuthorizeBillingPlatform(ctx, actor, "platform:audit"); err != nil {
		return domain.ReconciliationSummary{}, err
	}
	value, err := s.repository.BillingReconciliation(ctx, workspace, actor.UserID, currency)
	if err != nil {
		return domain.ReconciliationSummary{}, err
	}
	if value.WorkspaceID != workspace || value.Currency != currency || !value.Valid() {
		return domain.ReconciliationSummary{}, ErrBillingUnavailable
	}
	return value, nil
}

func (s *BillingService) Refund(ctx context.Context, actor BillingActor, request RefundRequest) (BillingReceipt, error) {
	if !validID(actor.UserID) || !validID(request.WorkspaceID) || !billingBusinessKey(request.BusinessKey) || !validID(request.RunID) || !validCurrency(request.Currency) || !validID(request.ApprovalID) || request.AmountMicro <= 0 || !billingReason(request.Reason) {
		return BillingReceipt{}, ErrBillingInvalid
	}
	if err := s.auth.AuthorizeBillingPlatform(ctx, actor, "platform:operate"); err != nil {
		return BillingReceipt{}, err
	}
	at, err := s.now()
	if err != nil {
		return BillingReceipt{}, err
	}
	receipt, err := s.repository.PostRefund(ctx, request, actor.UserID, at)
	if err != nil {
		return BillingReceipt{}, err
	}
	if !validID(receipt.JournalID) {
		return BillingReceipt{}, ErrBillingUnavailable
	}
	return receipt, nil
}

func (s *BillingService) Adjustment(ctx context.Context, actor BillingActor, request AdjustmentRequest) (BillingReceipt, error) {
	validBasis := request.BasisKind == "run" || request.BasisKind == "incident" || request.BasisKind == "reconciliation"
	if !validID(actor.UserID) || !validID(request.WorkspaceID) || !billingBusinessKey(request.BusinessKey) || !validBasis || !validID(request.BasisID) || (request.Direction != "debit" && request.Direction != "credit") || !validCurrency(request.Currency) || !validID(request.ApprovalID) || request.AmountMicro <= 0 || !billingReason(request.Reason) {
		return BillingReceipt{}, ErrBillingInvalid
	}
	if err := s.auth.AuthorizeBillingPlatform(ctx, actor, "platform:operate"); err != nil {
		return BillingReceipt{}, err
	}
	at, err := s.now()
	if err != nil {
		return BillingReceipt{}, err
	}
	receipt, err := s.repository.PostAdjustment(ctx, request, actor.UserID, at)
	if err != nil {
		return BillingReceipt{}, err
	}
	if !validID(receipt.JournalID) {
		return BillingReceipt{}, ErrBillingUnavailable
	}
	return receipt, nil
}
