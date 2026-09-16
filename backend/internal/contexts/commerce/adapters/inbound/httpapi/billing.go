package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
)

type BillingHandler struct {
	service *application.BillingService
	auth    application.BillingAuthorizer
}

func NewBilling(service *application.BillingService, auth application.BillingAuthorizer) (*BillingHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrBillingUnavailable
	}
	return &BillingHandler{service: service, auth: auth}, nil
}

func (h *BillingHandler) Register(router *gin.Engine) {
	base := "/api/admin/v1/workspaces/:workspace_id/billing"
	router.GET(base+"/summary", h.summary)
	router.GET(base+"/reconciliation", h.reconciliation)
	router.POST(base+"/refunds", h.refund)
	router.POST(base+"/adjustments", h.adjustment)
}

func billingFail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, application.ErrBillingInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Billing request is invalid."}})
	case errors.Is(err, application.ErrBillingUnauthenticated):
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHENTICATED", "message": "Login is required."}})
	case errors.Is(err, application.ErrBillingForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "Billing operation is not permitted."}})
	case errors.Is(err, application.ErrBillingNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "Billing basis was not found."}})
	case errors.Is(err, application.ErrBillingConflict):
		c.JSON(http.StatusConflict, gin.H{"error": gin.H{"code": "CONFLICT", "message": "Billing operation conflicts with immutable facts or approval."}})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": gin.H{"code": "TIMEOUT", "message": "Request deadline exceeded."}})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "BILLING_UNAVAILABLE", "message": "Billing service is temporarily unavailable."}})
	}
}

func billingActor(c *gin.Context, auth application.BillingAuthorizer, mutation bool) (context.Context, context.CancelFunc, application.BillingActor, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		billingFail(c, application.ErrBillingUnauthenticated)
		return ctx, cancel, application.BillingActor{}, false
	}
	var actor application.BillingActor
	if mutation {
		csrf := c.GetHeader("X-Mender-CSRF")
		if csrf == "" || len(csrf) > 256 {
			billingFail(c, application.ErrBillingForbidden)
			return ctx, cancel, application.BillingActor{}, false
		}
		actor, err = auth.AuthenticateBillingMutation(ctx, raw, csrf)
	} else {
		actor, err = auth.AuthenticateBilling(ctx, raw)
	}
	if err != nil {
		billingFail(c, err)
		return ctx, cancel, application.BillingActor{}, false
	}
	return ctx, cancel, actor, true
}

func singleCurrency(c *gin.Context) (string, bool) {
	values := c.Request.URL.Query()
	if len(values) != 1 || len(values["currency"]) != 1 {
		return "", false
	}
	return values.Get("currency"), true
}

func (h *BillingHandler) summary(c *gin.Context) {
	workspace := c.Param("workspace_id")
	currency, ok := singleCurrency(c)
	if !validID(workspace) || !ok {
		billingFail(c, application.ErrBillingInvalid)
		return
	}
	ctx, cancel, actor, ok := billingActor(c, h.auth, false)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.Summary(ctx, actor, workspace, currency)
	if err != nil {
		billingFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"workspace_id": v.WorkspaceID, "currency": v.Currency, "charged_micro": strconv.FormatInt(v.ChargedMicro, 10), "refunded_micro": strconv.FormatInt(v.RefundedMicro, 10), "adjustment_debit_micro": strconv.FormatInt(v.AdjustmentDebitMicro, 10), "adjustment_credit_micro": strconv.FormatInt(v.AdjustmentCreditMicro, 10), "net_billed_micro": strconv.FormatInt(v.NetBilledMicro, 10), "journal_count": strconv.FormatInt(v.JournalCount, 10), "latest_journal_at": v.LatestJournalAt}})
}

func (h *BillingHandler) reconciliation(c *gin.Context) {
	workspace := c.Param("workspace_id")
	currency, ok := singleCurrency(c)
	if !validID(workspace) || !ok {
		billingFail(c, application.ErrBillingInvalid)
		return
	}
	ctx, cancel, actor, ok := billingActor(c, h.auth, false)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.Reconciliation(ctx, actor, workspace, currency)
	if err != nil {
		billingFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"workspace_id": v.WorkspaceID, "currency": v.Currency, "usage_settlement_charged_micro": strconv.FormatInt(v.UsageSettlementChargedMicro, 10), "ledger_charge_micro": strconv.FormatInt(v.LedgerChargeMicro, 10), "missing_charge_journal_count": strconv.FormatInt(v.MissingChargeJournalCount, 10), "pending_reconcile_count": strconv.FormatInt(v.PendingReconcileCount, 10), "difference_micro": strconv.FormatInt(v.DifferenceMicro, 10)}})
}

type billingMutationInput struct {
	BusinessKey string `json:"business_key"`
	BasisKind   string `json:"basis_kind"`
	BasisID     string `json:"basis_id"`
	Direction   string `json:"direction"`
	AmountMicro string `json:"amount_micro"`
	Currency    string `json:"currency"`
	ApprovalID  string `json:"approval_id"`
	Reason      string `json:"reason"`
}

func decodeBilling(c *gin.Context, dst any) error {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 8192 {
		return application.ErrBillingInvalid
	}
	d := json.NewDecoder(io.LimitReader(c.Request.Body, 8193))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil || d.Decode(&struct{}{}) != io.EOF {
		return application.ErrBillingInvalid
	}
	return nil
}
func parseBillingAmount(value string) (int64, bool) {
	v, err := strconv.ParseInt(value, 10, 64)
	return v, err == nil && v > 0 && strconv.FormatInt(v, 10) == value
}

func (h *BillingHandler) refund(c *gin.Context) {
	workspace := c.Param("workspace_id")
	var in billingMutationInput
	if !validID(workspace) || decodeBilling(c, &in) != nil || in.BasisKind != "usage_settlement" || in.Direction != "credit" {
		billingFail(c, application.ErrBillingInvalid)
		return
	}
	amount, ok := parseBillingAmount(in.AmountMicro)
	if !ok {
		billingFail(c, application.ErrBillingInvalid)
		return
	}
	ctx, cancel, actor, ok := billingActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.Refund(ctx, actor, application.RefundRequest{WorkspaceID: workspace, BusinessKey: in.BusinessKey, RunID: in.BasisID, AmountMicro: amount, Currency: in.Currency, ApprovalID: in.ApprovalID, Reason: in.Reason})
	if err != nil {
		billingFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"journal_id": v.JournalID, "replay": v.Replay}})
}

func (h *BillingHandler) adjustment(c *gin.Context) {
	workspace := c.Param("workspace_id")
	var in billingMutationInput
	if !validID(workspace) || decodeBilling(c, &in) != nil {
		billingFail(c, application.ErrBillingInvalid)
		return
	}
	amount, ok := parseBillingAmount(in.AmountMicro)
	if !ok {
		billingFail(c, application.ErrBillingInvalid)
		return
	}
	ctx, cancel, actor, ok := billingActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.Adjustment(ctx, actor, application.AdjustmentRequest{WorkspaceID: workspace, BusinessKey: in.BusinessKey, BasisKind: in.BasisKind, BasisID: in.BasisID, Direction: in.Direction, AmountMicro: amount, Currency: in.Currency, ApprovalID: in.ApprovalID, Reason: in.Reason})
	if err != nil {
		billingFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"journal_id": v.JournalID, "replay": v.Replay}})
}
