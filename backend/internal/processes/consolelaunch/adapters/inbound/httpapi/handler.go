package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/processes/consolelaunch/application"
)

const sessionCookie = "mender_session"

type Handler struct{ service *application.Service }

func New(service *application.Service) (*Handler, error) {
	if service == nil {
		return nil, application.ErrUnavailable
	}
	return &Handler{service: service}, nil
}

func (h *Handler) Register(router *gin.Engine) {
	router.GET("/api/console/v1/workspaces/:workspace_id/launch-options", h.list)
}

func fail(c *gin.Context, err error) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	switch {
	case errors.Is(err, application.ErrUnauthenticated):
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHENTICATED", "message": "Console login is required."}})
	case errors.Is(err, application.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "Workspace launch discovery is not permitted."}})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": gin.H{"code": "TIMEOUT", "message": "Launch discovery timed out."}})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "DEPENDENCY_UNAVAILABLE", "message": "Launch discovery is unavailable."}})
	}
}

func (h *Handler) list(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if c.Request.URL.RawQuery != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Launch discovery query is invalid."}})
		return
	}
	raw, err := c.Cookie(sessionCookie)
	if err != nil || raw == "" {
		fail(c, application.ErrUnauthenticated)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	actor, err := h.service.Authenticate(ctx, raw)
	if err != nil {
		fail(c, err)
		return
	}
	items, err := h.service.List(ctx, actor, c.Param("workspace_id"))
	if err != nil {
		fail(c, err)
		return
	}
	data := make([]gin.H, 0, len(items))
	for _, item := range items {
		data = append(data, gin.H{
			"toolset_version_id": item.ToolsetVersionID, "tool_id": item.ToolID, "tool_version": item.ToolVersion, "tool_version_id": item.ToolVersionID,
			"title": item.Title, "description": item.Description, "input_schema": json.RawMessage(item.InputSchema), "side_effect": item.SideEffect, "idempotency": item.Idempotency,
			"connection_id": item.ConnectionID, "provider_id": item.ProviderID, "currency": item.Currency, "reserve_micro": strconv.FormatInt(item.ReserveMicro, 10),
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}
