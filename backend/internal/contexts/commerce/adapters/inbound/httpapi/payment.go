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

type PaymentAdminHandler struct {
	service *application.PaymentAdminService
	auth    application.BillingAuthorizer
}

func NewPaymentAdmin(service *application.PaymentAdminService, auth application.BillingAuthorizer) (*PaymentAdminHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrBillingUnavailable
	}
	return &PaymentAdminHandler{service: service, auth: auth}, nil
}

func (h *PaymentAdminHandler) Register(router *gin.Engine) {
	base := "/api/admin/v1/workspaces/:workspace_id/payments"
	router.POST(base+"/intents", h.createIntent)
	router.GET(base+"/intents", h.intents)
	router.GET(base+"/callbacks", h.callbacks)
	router.GET(base+"/reconciliation", h.reconciliation)
}

type paymentIntentInput struct {
	BusinessKey       string `json:"business_key"`
	ProviderID        string `json:"provider_id"`
	ProviderAccountID string `json:"provider_account_id"`
	Mode              string `json:"mode"`
	Purpose           string `json:"purpose"`
	BillingJournalID  string `json:"billing_journal_id"`
	Currency          string `json:"currency"`
	AmountMicro       string `json:"amount_micro"`
}

func decodePaymentAdmin(c *gin.Context, dst any) error {
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

func paymentQuery(c *gin.Context, currency bool) (provider, account, curr string, ok bool) {
	values := c.Request.URL.Query()
	want := 2
	if currency {
		want = 3
	}
	if len(values) != want || len(values["provider_id"]) != 1 || len(values["provider_account_id"]) != 1 || currency && len(values["currency"]) != 1 {
		return "", "", "", false
	}
	return values.Get("provider_id"), values.Get("provider_account_id"), values.Get("currency"), true
}

func (h *PaymentAdminHandler) createIntent(c *gin.Context) {
	workspace := c.Param("workspace_id")
	var input paymentIntentInput
	if !validID(workspace) || decodePaymentAdmin(c, &input) != nil {
		billingFail(c, application.ErrBillingInvalid)
		return
	}
	amount, ok := parseBillingAmount(input.AmountMicro)
	if !ok {
		billingFail(c, application.ErrBillingInvalid)
		return
	}
	ctx, cancel, actor, ok := billingActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	receipt, err := h.service.CreateIntent(ctx, actor, application.PaymentIntentRequest{
		WorkspaceID: workspace, BusinessKey: input.BusinessKey, ProviderID: input.ProviderID, ProviderAccountID: input.ProviderAccountID,
		Mode: input.Mode, Purpose: input.Purpose, BillingJournalID: input.BillingJournalID, Currency: input.Currency, AmountMicro: amount,
	})
	if err != nil {
		billingFail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": gin.H{"intent_id": receipt.IntentID, "replay": receipt.Replay, "mode": "sandbox"}})
}

func (h *PaymentAdminHandler) intents(c *gin.Context) {
	workspace := c.Param("workspace_id")
	provider, account, _, ok := paymentQuery(c, false)
	if !validID(workspace) || !ok {
		billingFail(c, application.ErrBillingInvalid)
		return
	}
	ctx, cancel, actor, ok := billingActor(c, h.auth, false)
	defer cancel()
	if !ok {
		return
	}
	items, err := h.service.Intents(ctx, actor, workspace, provider, account)
	if err != nil {
		billingFail(c, err)
		return
	}
	data := make([]gin.H, 0, len(items))
	for _, v := range items {
		data = append(data, gin.H{"id": v.ID, "business_key": v.BusinessKey, "purpose": v.Purpose, "billing_journal_id": v.BillingJournalID,
			"currency": v.Currency, "amount_micro": strconv.FormatInt(v.AmountMicro, 10), "state": v.State, "revision": strconv.FormatInt(v.Revision, 10),
			"created_at": v.CreatedAt, "updated_at": v.UpdatedAt, "settled_at": nullablePaymentTime(v.SettledAt), "failed_at": nullablePaymentTime(v.FailedAt)})
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "meta": gin.H{"mode": "sandbox"}})
}

func (h *PaymentAdminHandler) callbacks(c *gin.Context) {
	workspace := c.Param("workspace_id")
	provider, account, _, ok := paymentQuery(c, false)
	if !validID(workspace) || !ok {
		billingFail(c, application.ErrBillingInvalid)
		return
	}
	ctx, cancel, actor, ok := billingActor(c, h.auth, false)
	defer cancel()
	if !ok {
		return
	}
	items, err := h.service.Callbacks(ctx, actor, workspace, provider, account)
	if err != nil {
		billingFail(c, err)
		return
	}
	data := make([]gin.H, 0, len(items))
	for _, v := range items {
		data = append(data, gin.H{"receipt_id": v.ReceiptID, "event_id": v.EventID, "intent_id": v.IntentID, "event_type": v.EventType,
			"currency": v.Currency, "amount_micro": strconv.FormatInt(v.AmountMicro, 10), "event_state": v.EventState,
			"disposition": v.Disposition, "reason_code": nullablePaymentString(v.ReasonCode), "occurred_at": v.OccurredAt, "received_at": v.ReceivedAt,
			"delivery_count": strconv.Itoa(v.DeliveryCount)})
	}
	c.JSON(http.StatusOK, gin.H{"data": data, "meta": gin.H{"mode": "sandbox"}})
}

func (h *PaymentAdminHandler) reconciliation(c *gin.Context) {
	workspace := c.Param("workspace_id")
	provider, account, currency, ok := paymentQuery(c, true)
	if !validID(workspace) || !ok {
		billingFail(c, application.ErrBillingInvalid)
		return
	}
	ctx, cancel, actor, ok := billingActor(c, h.auth, false)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.Reconciliation(ctx, actor, workspace, provider, account, currency)
	if err != nil {
		billingFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"workspace_id": v.WorkspaceID, "provider_id": v.ProviderID, "provider_account_id": v.ProviderAccountID, "currency": v.Currency,
		"expected_collection_micro": strconv.FormatInt(v.ExpectedCollectionMicro, 10), "settled_collection_micro": strconv.FormatInt(v.SettledCollectionMicro, 10),
		"expected_refund_micro": strconv.FormatInt(v.ExpectedRefundMicro, 10), "settled_refund_micro": strconv.FormatInt(v.SettledRefundMicro, 10),
		"pending_intent_count": strconv.FormatInt(v.PendingIntentCount, 10), "quarantined_event_count": strconv.FormatInt(v.QuarantinedEventCount, 10),
		"collection_difference_micro": strconv.FormatInt(v.CollectionDifferenceMicro, 10), "refund_difference_micro": strconv.FormatInt(v.RefundDifferenceMicro, 10),
	}, "meta": gin.H{"mode": "sandbox", "live_payments_enabled": false}})
}

func nullablePaymentTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nullablePaymentString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

type PaymentCallbackHandler struct {
	service *application.PaymentCallbackService
}

func NewPaymentCallback(service *application.PaymentCallbackService) (*PaymentCallbackHandler, error) {
	if service == nil {
		return nil, application.ErrPaymentCallbackUnavailable
	}
	return &PaymentCallbackHandler{service: service}, nil
}

func (h *PaymentCallbackHandler) Register(router *gin.Engine) {
	router.POST("/api/payment-callbacks/v1/providers/:provider_id/accounts/:provider_account_id", h.handle)
}

func (h *PaymentCallbackHandler) handle(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if c.Request.URL.RawQuery != "" || c.ContentType() != "application/json" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_CALLBACK", "message": "Payment callback request is invalid."}})
		return
	}
	body := http.MaxBytesReader(c.Writer, c.Request.Body, application.MaxPaymentCallbackBodyBytes)
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": gin.H{"code": "BODY_TOO_LARGE", "message": "Payment callback body exceeds limit."}})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_CALLBACK", "message": "Payment callback body is invalid."}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	receipt, err := h.service.Handle(ctx, c.Param("provider_id"), c.Param("provider_account_id"), c.GetHeader("X-Mender-Payment-Key-Id"), c.GetHeader("X-Mender-Payment-Timestamp"), c.GetHeader("X-Mender-Payment-Signature"), raw)
	if err != nil {
		switch {
		case errors.Is(err, application.ErrPaymentCallbackUnauthorized):
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "INVALID_SIGNATURE", "message": "Payment callback signature is invalid."}})
		case errors.Is(err, application.ErrPaymentCallbackInvalid):
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_CALLBACK", "message": "Payment callback payload is invalid."}})
		case errors.Is(err, context.DeadlineExceeded):
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": gin.H{"code": "TIMEOUT", "message": "Payment callback processing timed out."}})
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "PAYMENT_CALLBACK_UNAVAILABLE", "message": "Payment callback processing is unavailable."}})
		}
		return
	}
	status := http.StatusAccepted
	if receipt.Disposition == "duplicate" {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"data": gin.H{"event_id": receipt.EventID, "disposition": receipt.Disposition}})
}
