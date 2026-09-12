package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/commerce/application"
)

const sessionCookie = "mender_session"

type UsageHandler struct {
	service    *application.UsageService
	authorizer application.UsageAuthorizer
}

func NewUsage(service *application.UsageService, authorizer application.UsageAuthorizer) (*UsageHandler, error) {
	if service == nil || authorizer == nil {
		return nil, application.ErrObservabilityUnavailable
	}
	return &UsageHandler{service: service, authorizer: authorizer}, nil
}

func (h *UsageHandler) Register(router *gin.Engine) {
	router.GET("/api/console/v1/workspaces/:workspace_id/usage", h.get)
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func usageFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, application.ErrObservabilityUnauthenticated):
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHENTICATED", "message": "Login is required."}})
	case errors.Is(err, application.ErrObservabilityForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "Usage visibility is not permitted."}})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": gin.H{"code": "TIMEOUT", "message": "Request deadline exceeded."}})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "USAGE_UNAVAILABLE", "message": "Usage data is temporarily unavailable."}})
	}
}

type budgetDTO struct {
	BudgetID, PeriodID, Currency                             string
	StartsAt, EndsAt                                         time.Time
	Active                                                   bool
	LimitMicro, ConsumedMicro, ReservedMicro, AvailableMicro string
	Revision                                                 string
}

type usageDTO struct {
	RunID, BudgetID, PeriodID, Currency, QuotaState string
	ReservedMicro, ReleasedMicro                    string
	ChargedMicro                                    *string
	Outcome                                         *string
	CreatedAt                                       time.Time
	FinalizedAt                                     *time.Time
}

func budgetView(v application.BudgetPeriodView) gin.H {
	return gin.H{
		"budget_id": v.BudgetID, "period_id": v.PeriodID, "currency": v.Currency,
		"starts_at": v.StartsAt, "ends_at": v.EndsAt, "active": v.Active,
		"limit_micro": strconv.FormatInt(v.LimitMicro, 10), "consumed_micro": strconv.FormatInt(v.ConsumedMicro, 10),
		"reserved_micro": strconv.FormatInt(v.ReservedMicro, 10), "available_micro": strconv.FormatInt(v.AvailableMicro(), 10),
		"revision": strconv.FormatInt(v.Revision, 10),
	}
}

func usageView(v application.UsageEntryView) gin.H {
	var charged any
	if v.ChargedMicro != nil {
		charged = strconv.FormatInt(*v.ChargedMicro, 10)
	}
	var finalized any
	if !v.FinalizedAt.IsZero() {
		finalized = v.FinalizedAt
	}
	var outcome any
	if v.Outcome != "" {
		outcome = v.Outcome
	}
	return gin.H{
		"run_id": v.RunID, "budget_id": v.BudgetID, "period_id": v.PeriodID, "currency": v.Currency,
		"quota_state": v.QuotaState, "reserved_micro": strconv.FormatInt(v.ReservedMicro, 10),
		"charged_micro": charged, "released_micro": strconv.FormatInt(v.ReleasedMicro(), 10), "outcome": outcome,
		"created_at": v.CreatedAt, "finalized_at": finalized,
	}
}

func (h *UsageHandler) get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	if c.Request.URL.RawQuery != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Usage query is invalid."}})
		return
	}
	workspace := c.Param("workspace_id")
	if !validID(workspace) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Workspace is invalid."}})
		return
	}
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		usageFailure(c, application.ErrObservabilityUnauthenticated)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	actor, err := h.authorizer.Authenticate(ctx, raw)
	if err != nil {
		usageFailure(c, err)
		return
	}
	result, err := h.service.Snapshot(ctx, actor, workspace)
	if err != nil {
		usageFailure(c, err)
		return
	}
	periods := make([]gin.H, 0, len(result.BudgetPeriods))
	for _, period := range result.BudgetPeriods {
		periods = append(periods, budgetView(period))
	}
	entries := make([]gin.H, 0, len(result.Entries))
	for _, entry := range result.Entries {
		entries = append(entries, usageView(entry))
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"budget_periods": periods, "usage_entries": entries}})
}
