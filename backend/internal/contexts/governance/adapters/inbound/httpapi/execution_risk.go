package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
	"github.com/orz-i/mender/backend/internal/sharedkernel/canonicaljson"
)

type ExecutionRiskHandler struct {
	service *application.ExecutionRiskHumanService
	auth    application.Authorizer
}

func NewExecutionRisk(service *application.ExecutionRiskHumanService, auth application.Authorizer) (*ExecutionRiskHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrUnavailable
	}
	return &ExecutionRiskHandler{service: service, auth: auth}, nil
}

func (h *ExecutionRiskHandler) Register(router *gin.Engine) {
	base := "/api/console/v1/workspaces/:workspace_id/execution-risk"
	router.POST(base+"/preview", h.preview)
	router.POST(base+"/confirm", h.confirm)
}

type executionRiskInput struct {
	ToolsetVersionID string          `json:"toolset_version_id"`
	ToolVersionID    string          `json:"tool_version_id"`
	ConnectionID     string          `json:"connection_id"`
	IdempotencyKey   string          `json:"idempotency_key"`
	Arguments        json.RawMessage `json:"arguments"`
}

func validExecutionIdempotency(value string) bool {
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for _, ch := range value {
		if ch < '!' || ch > '~' {
			return false
		}
	}
	return true
}

func decodeExecutionRisk(c *gin.Context) (executionRiskInput, error) {
	var input executionRiskInput
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 70<<10 {
		return input, application.ErrInvalid
	}
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, (70<<10)+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return input, application.ErrInvalid
	}
	if !validID(input.ToolsetVersionID) || !validID(input.ToolVersionID) || !validID(input.ConnectionID) || !validExecutionIdempotency(input.IdempotencyKey) {
		return input, application.ErrInvalid
	}
	return input, nil
}

func target(input executionRiskInput) (application.ExecutionRiskTarget, error) {
	canonical, err := canonicaljson.Object(input.Arguments, 65536)
	if err != nil {
		return application.ExecutionRiskTarget{}, application.ErrInvalid
	}
	argumentsSum := sha256.Sum256(canonical)
	idempotencySum := sha256.Sum256([]byte(input.IdempotencyKey))
	return application.ExecutionRiskTarget{
		ToolsetVersionID: input.ToolsetVersionID, ToolVersionID: input.ToolVersionID, ConnectionID: input.ConnectionID,
		ArgumentsHash: hex.EncodeToString(argumentsSum[:]), IdempotencyKeyHash: hex.EncodeToString(idempotencySum[:]),
	}, nil
}

func executionFail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, application.ErrUnauthenticated):
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHENTICATED", "message": "Login is required."}})
	case errors.Is(err, application.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "Execution risk operation is not permitted."}})
	case errors.Is(err, application.ErrInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Execution risk request is invalid."}})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": gin.H{"code": "TIMEOUT", "message": "Request deadline exceeded."}})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "GOVERNANCE_UNAVAILABLE", "message": "Execution risk governance is temporarily unavailable."}})
	}
}

func (h *ExecutionRiskHandler) actor(c *gin.Context) (context.Context, context.CancelFunc, application.Actor, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		executionFail(c, application.ErrUnauthenticated)
		return ctx, cancel, application.Actor{}, false
	}
	csrf := c.GetHeader("X-Mender-CSRF")
	if csrf == "" || len(csrf) > 256 {
		executionFail(c, application.ErrForbidden)
		return ctx, cancel, application.Actor{}, false
	}
	actor, err := h.auth.AuthenticateMutation(ctx, raw, csrf)
	if err != nil {
		executionFail(c, err)
		return ctx, cancel, application.Actor{}, false
	}
	return ctx, cancel, actor, true
}

func executionDecisionView(item application.ExecutionRiskDecision) gin.H {
	return gin.H{
		"sequence": strconv.FormatInt(item.Sequence, 10), "policy_revision_id": item.PolicyRevisionID,
		"policy_revision": strconv.FormatInt(item.PolicyRevision, 10), "toolset_version_id": item.ToolsetVersionID,
		"tool_version_id": item.ToolVersionID, "connection_id": item.ConnectionID, "arguments_hash": item.ArgumentsHash,
		"risk_level": item.RiskLevel, "outcome": item.Outcome, "reason_codes": item.ReasonCodes, "evaluated_at": item.EvaluatedAt,
	}
}

func (h *ExecutionRiskHandler) preview(c *gin.Context) {
	configure(c)
	workspace := c.Param("workspace_id")
	if !validID(workspace) {
		executionFail(c, application.ErrInvalid)
		return
	}
	input, err := decodeExecutionRisk(c)
	if err != nil {
		executionFail(c, err)
		return
	}
	target, err := target(input)
	if err != nil {
		executionFail(c, err)
		return
	}
	ctx, cancel, actor, ok := h.actor(c)
	defer cancel()
	if !ok {
		return
	}
	decision, err := h.service.Preview(ctx, actor, workspace, target)
	if err != nil {
		executionFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": executionDecisionView(decision)})
}

func (h *ExecutionRiskHandler) confirm(c *gin.Context) {
	configure(c)
	workspace := c.Param("workspace_id")
	if !validID(workspace) {
		executionFail(c, application.ErrInvalid)
		return
	}
	input, err := decodeExecutionRisk(c)
	if err != nil {
		executionFail(c, err)
		return
	}
	target, err := target(input)
	if err != nil {
		executionFail(c, err)
		return
	}
	ctx, cancel, actor, ok := h.actor(c)
	defer cancel()
	if !ok {
		return
	}
	result, err := h.service.Confirm(ctx, actor, workspace, target)
	if err != nil {
		executionFail(c, err)
		return
	}
	var confirmationID, expiresAt any
	if result.ConfirmationID != "" {
		confirmationID, expiresAt = result.ConfirmationID, result.ExpiresAt
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"decision": executionDecisionView(result.Decision), "confirmation_id": confirmationID, "expires_at": expiresAt}})
}
