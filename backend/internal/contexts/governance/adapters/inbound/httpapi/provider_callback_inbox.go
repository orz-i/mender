package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type ProviderCallbackInboxHandler struct {
	service *application.ProviderCallbackInboxService
	auth    application.Authorizer
}

func NewProviderCallbackInbox(service *application.ProviderCallbackInboxService, auth application.Authorizer) (*ProviderCallbackInboxHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrUnavailable
	}
	return &ProviderCallbackInboxHandler{service: service, auth: auth}, nil
}

func (h *ProviderCallbackInboxHandler) Register(router *gin.Engine) {
	router.GET("/api/admin/v1/workspaces/:workspace_id/provider-callbacks", h.list)
}

func providerCallbackInboxFilter(c *gin.Context) (application.ProviderCallbackInboxFilter, error) {
	filter := application.ProviderCallbackInboxFilter{Limit: 50}
	allowed := map[string]bool{"provider_id": true, "disposition": true, "reason_code": true, "before_received_at": true, "before_receipt_id": true, "limit": true}
	values := c.Request.URL.Query()
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return filter, application.ErrInvalid
		}
	}
	filter.ProviderID, filter.Disposition, filter.ReasonCode = values.Get("provider_id"), values.Get("disposition"), values.Get("reason_code")
	beforeAt, beforeID := values.Get("before_received_at"), values.Get("before_receipt_id")
	if (beforeAt == "") != (beforeID == "") {
		return filter, application.ErrInvalid
	}
	if beforeAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, beforeAt)
		if err != nil {
			return filter, application.ErrInvalid
		}
		filter.BeforeReceivedAt, filter.BeforeReceiptID = parsed.UTC(), beforeID
	}
	if raw := values.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 || strconv.Itoa(value) != raw {
			return filter, application.ErrInvalid
		}
		filter.Limit = value
	}
	return filter, nil
}

func (h *ProviderCallbackInboxHandler) list(c *gin.Context) {
	configure(c)
	workspace := c.Param("workspace_id")
	if !validID(workspace) {
		fail(c, application.ErrInvalid)
		return
	}
	filter, err := providerCallbackInboxFilter(c)
	if err != nil {
		fail(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		fail(c, application.ErrUnauthenticated)
		return
	}
	actor, err := h.auth.Authenticate(ctx, raw)
	if err != nil {
		fail(c, err)
		return
	}
	page, err := h.service.List(ctx, actor, workspace, filter)
	if err != nil {
		fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, gin.H{
			"receipt_id": item.ReceiptID, "workspace_id": item.WorkspaceID, "provider_id": item.ProviderID, "event_id": item.EventID,
			"run_id": item.RunID, "observation_id": item.ObservationID, "observation_state": item.ObservationState,
			"disposition": item.Disposition, "reason_code": nullableString(item.ReasonCode), "delivery_count": item.DeliveryCount,
			"duplicate_delivery_count": item.DuplicateDeliveryCount, "received_at": item.ReceivedAt, "last_received_at": item.LastReceivedAt,
			"observed_at": item.ObservedAt, "processed_at": nullableTime(item.ProcessedAt),
		})
	}
	var next any
	if !page.NextBeforeReceivedAt.IsZero() {
		next = gin.H{"received_at": page.NextBeforeReceivedAt, "receipt_id": page.NextBeforeReceiptID}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"items": items, "next_cursor": next}})
}
