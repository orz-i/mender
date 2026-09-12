package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/identity/application"
)

type RunDelegationHandler struct {
	sessions    *application.HumanSessionService
	delegations *application.RunDelegationService
}

func NewRunDelegationHandler(sessions *application.HumanSessionService, delegations *application.RunDelegationService) (*RunDelegationHandler, error) {
	if sessions == nil || delegations == nil {
		return nil, errors.New("run delegation HTTP adapter is not configured")
	}
	return &RunDelegationHandler{sessions: sessions, delegations: delegations}, nil
}

func (h *RunDelegationHandler) Register(router *gin.Engine) {
	base := "/api/console/v1/workspaces/:workspace_id/run-delegations"
	router.POST(base, h.issue)
	router.DELETE(base+"/:delegation_id", h.revoke)
}

type delegationRequest struct {
	Scopes []string `json:"scopes"`
}

func decodeDelegationRequest(body io.Reader) (delegationRequest, error) {
	d := json.NewDecoder(body)
	d.DisallowUnknownFields()
	var request delegationRequest
	if err := d.Decode(&request); err != nil {
		return delegationRequest{}, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err != nil {
			return delegationRequest{}, err
		}
		return delegationRequest{}, errors.New("trailing JSON")
	}
	return request, nil
}

func delegationFailure(c *gin.Context, err error) {
	switch {
	case errors.Is(err, application.ErrUnauthenticated):
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "UNAUTHENTICATED", "message": "Login is required."}})
	case errors.Is(err, application.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "Run delegation is not permitted."}})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": gin.H{"code": "TIMEOUT", "message": "Request deadline exceeded."}})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "DELEGATION_UNAVAILABLE", "message": "Run delegation is temporarily unavailable."}})
	}
}

func delegationHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
}

func (h *RunDelegationHandler) humanMutation(c *gin.Context) (context.Context, context.CancelFunc, application.HumanPrincipal, bool) {
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

func (h *RunDelegationHandler) issue(c *gin.Context) {
	delegationHeaders(c)
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength <= 0 || c.Request.ContentLength > 2048 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Delegation request is invalid."}})
		return
	}
	request, err := decodeDelegationRequest(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Delegation request is invalid."}})
		return
	}
	workspace := c.Param("workspace_id")
	ctx, cancel, principal, ok := h.humanMutation(c)
	defer cancel()
	if !ok {
		return
	}
	issued, err := h.delegations.Issue(ctx, principal, workspace, request.Scopes)
	if err != nil {
		delegationFailure(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": gin.H{
		"delegation_id": issued.DelegationID,
		"workspace_id":  issued.WorkspaceID,
		"token":         issued.Token,
		"scopes":        issued.Scopes,
		"expires_at":    issued.ExpiresAt,
	}})
}

func (h *RunDelegationHandler) revoke(c *gin.Context) {
	delegationHeaders(c)
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Delegation revoke request is invalid."}})
		return
	}
	ctx, cancel, principal, ok := h.humanMutation(c)
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
