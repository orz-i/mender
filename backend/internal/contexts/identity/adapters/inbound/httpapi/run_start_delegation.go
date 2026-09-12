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
	"github.com/orz-i/mender/backend/internal/contexts/identity/application"
)

type RunStartDelegationHandler struct {
	sessions    *application.HumanSessionService
	delegations *application.RunStartDelegationService
}

func NewRunStartDelegationHandler(sessions *application.HumanSessionService, delegations *application.RunStartDelegationService) (*RunStartDelegationHandler, error) {
	if sessions == nil || delegations == nil {
		return nil, errors.New("run start delegation HTTP adapter is not configured")
	}
	return &RunStartDelegationHandler{sessions: sessions, delegations: delegations}, nil
}

func (h *RunStartDelegationHandler) Register(router *gin.Engine) {
	base := "/api/console/v1/workspaces/:workspace_id/run-start-delegations"
	router.POST(base, h.issue)
	router.DELETE(base+"/:delegation_id", h.revoke)
}

type runStartDelegationRequest struct {
	ToolsetVersionID string `json:"toolset_version_id"`
	ToolID           string `json:"tool_id"`
	ToolVersion      string `json:"tool_version"`
	ToolVersionID    string `json:"tool_version_id"`
	ConnectionID     string `json:"connection_id"`
	Currency         string `json:"currency"`
	MaxChargeMicro   string `json:"max_charge_micro"`
	IdempotencyKey   string `json:"idempotency_key"`
}

func decodeRunStartDelegation(body io.Reader) (runStartDelegationRequest, error) {
	d := json.NewDecoder(io.LimitReader(body, 4097))
	d.DisallowUnknownFields()
	var request runStartDelegationRequest
	if err := d.Decode(&request); err != nil {
		return runStartDelegationRequest{}, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err != nil {
			return runStartDelegationRequest{}, err
		}
		return runStartDelegationRequest{}, errors.New("trailing JSON")
	}
	return request, nil
}

func parseStartCap(raw string) (int64, error) {
	if raw == "" || len(raw) > 19 || len(raw) > 1 && raw[0] == '0' {
		return 0, application.ErrForbidden
	}
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, application.ErrForbidden
		}
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, application.ErrForbidden
	}
	return value, nil
}

func (h *RunStartDelegationHandler) mutationPrincipal(c *gin.Context) (context.Context, context.CancelFunc, application.HumanPrincipal, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		delegationFailure(c, application.ErrUnauthenticated)
		return ctx, cancel, application.HumanPrincipal{}, false
	}
	principal, err := h.sessions.Authenticate(ctx, raw)
	if err != nil {
		delegationFailure(c, err)
		return ctx, cancel, application.HumanPrincipal{}, false
	}
	csrf := c.GetHeader("X-Mender-CSRF")
	if csrf == "" || len(csrf) > 256 || h.sessions.VerifyCSRF(principal, csrf) != nil {
		delegationFailure(c, application.ErrForbidden)
		return ctx, cancel, application.HumanPrincipal{}, false
	}
	return ctx, cancel, principal, true
}

func (h *RunStartDelegationHandler) issue(c *gin.Context) {
	delegationHeaders(c)
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength <= 0 || c.Request.ContentLength > 4096 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Start delegation request is invalid."}})
		return
	}
	request, err := decodeRunStartDelegation(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Start delegation request is invalid."}})
		return
	}
	maxCharge, err := parseStartCap(request.MaxChargeMicro)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Start delegation request is invalid."}})
		return
	}
	ctx, cancel, principal, ok := h.mutationPrincipal(c)
	defer cancel()
	if !ok {
		return
	}
	issued, err := h.delegations.Issue(ctx, principal, c.Param("workspace_id"), application.RunStartConstraint{
		ToolsetVersionID: request.ToolsetVersionID,
		ToolID:           request.ToolID,
		ToolVersion:      request.ToolVersion,
		ToolVersionID:    request.ToolVersionID,
		ConnectionID:     request.ConnectionID,
		Currency:         request.Currency,
		MaxChargeMicro:   maxCharge,
		IdempotencyKey:   request.IdempotencyKey,
	})
	if err != nil {
		delegationFailure(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": gin.H{
		"delegation_id":      issued.DelegationID,
		"workspace_id":       issued.WorkspaceID,
		"token":              issued.Token,
		"toolset_version_id": issued.Constraint.ToolsetVersionID,
		"tool_id":            issued.Constraint.ToolID,
		"tool_version":       issued.Constraint.ToolVersion,
		"tool_version_id":    issued.Constraint.ToolVersionID,
		"connection_id":      issued.Constraint.ConnectionID,
		"currency":           issued.Constraint.Currency,
		"max_charge_micro":   strconv.FormatInt(issued.Constraint.MaxChargeMicro, 10),
		"idempotency_key":    issued.Constraint.IdempotencyKey,
		"expires_at":         issued.ExpiresAt,
	}})
}

func (h *RunStartDelegationHandler) revoke(c *gin.Context) {
	delegationHeaders(c)
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Start delegation revoke request is invalid."}})
		return
	}
	ctx, cancel, principal, ok := h.mutationPrincipal(c)
	defer cancel()
	if !ok {
		return
	}
	if err := h.delegations.Revoke(ctx, principal, c.Param("workspace_id"), c.Param("delegation_id")); err != nil {
		delegationFailure(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
