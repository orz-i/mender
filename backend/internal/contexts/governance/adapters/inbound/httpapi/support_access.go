package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type SupportAccessHandler struct {
	service *application.SupportAccessService
	auth    application.PlatformAuthorizer
}

func NewSupportAccess(service *application.SupportAccessService, auth application.PlatformAuthorizer) (*SupportAccessHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrUnavailable
	}
	return &SupportAccessHandler{service: service, auth: auth}, nil
}

func (h *SupportAccessHandler) Register(router *gin.Engine) {
	base := "/api/admin/v1/support/workspaces/:workspace_id"
	router.POST(base+"/jit-requests", h.request)
	router.POST(base+"/jit-requests/:approval_id/approve", h.approve)
	router.POST(base+"/jit-requests/:approval_id/reject", h.reject)
	router.POST(base+"/jit-requests/:approval_id/activate", h.activate)
	router.POST(base+"/jit-grants/:grant_id/revoke", h.revoke)
	router.GET(base+"/runs", h.runs)
}

func supportActor(c *gin.Context, auth application.PlatformAuthorizer, mutation bool) (context.Context, context.CancelFunc, application.Actor, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		fail(c, application.ErrUnauthenticated)
		return ctx, cancel, application.Actor{}, false
	}
	var actor application.Actor
	if mutation {
		csrf := c.GetHeader("X-Mender-CSRF")
		if csrf == "" || len(csrf) > 256 {
			fail(c, application.ErrForbidden)
			return ctx, cancel, application.Actor{}, false
		}
		actor, err = auth.AuthenticateMutation(ctx, raw, csrf)
	} else {
		actor, err = auth.Authenticate(ctx, raw)
	}
	if err != nil {
		fail(c, err)
		return ctx, cancel, application.Actor{}, false
	}
	return ctx, cancel, actor, true
}

func decodeSupport(c *gin.Context, dst any) error {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 8192 {
		return application.ErrInvalid
	}
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 8193))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return application.ErrInvalid
	}
	return nil
}

type supportRequestInput struct {
	Scopes     []string `json:"scopes"`
	TTLSeconds int64    `json:"ttl_seconds"`
	Reason     string   `json:"reason"`
}

func (h *SupportAccessHandler) request(c *gin.Context) {
	configure(c)
	workspace := c.Param("workspace_id")
	var input supportRequestInput
	if !validID(workspace) || decodeSupport(c, &input) != nil || input.TTLSeconds < 300 || input.TTLSeconds > 3600 {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := supportActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	item, err := h.service.Request(ctx, actor, workspace, input.Scopes, time.Duration(input.TTLSeconds)*time.Second, input.Reason)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": dangerousView(item)})
}

func (h *SupportAccessHandler) approve(c *gin.Context) { h.review(c, true) }
func (h *SupportAccessHandler) reject(c *gin.Context)  { h.review(c, false) }

func (h *SupportAccessHandler) review(c *gin.Context, approve bool) {
	configure(c)
	workspace, id := c.Param("workspace_id"), c.Param("approval_id")
	var input decisionInput
	if !validID(workspace) || !validID(id) || decodeSupport(c, &input) != nil {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := supportActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	var item application.DangerousOperationApproval
	var err error
	if approve {
		item, err = h.service.Approve(ctx, actor, workspace, id, input.Note)
	} else {
		item, err = h.service.Reject(ctx, actor, workspace, id, input.Note)
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": dangerousView(item)})
}

func supportGrantView(v application.JITSupportGrant) gin.H {
	return gin.H{
		"id": v.ID, "approval_id": v.ApprovalID, "user_id": v.UserID, "scopes": v.Scopes, "reason": v.Reason,
		"created_at": v.CreatedAt, "expires_at": v.ExpiresAt, "revoked_at": nullableTime(v.RevokedAt),
	}
}

func (h *SupportAccessHandler) activate(c *gin.Context) {
	configure(c)
	workspace, id := c.Param("workspace_id"), c.Param("approval_id")
	var input struct{}
	if !validID(workspace) || !validID(id) || decodeSupport(c, &input) != nil {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := supportActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	grant, err := h.service.Activate(ctx, actor, workspace, id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": supportGrantView(grant)})
}

type supportRevokeInput struct{ Reason string `json:"reason"` }

func (h *SupportAccessHandler) revoke(c *gin.Context) {
	configure(c)
	workspace, id := c.Param("workspace_id"), c.Param("grant_id")
	var input supportRevokeInput
	if !validID(workspace) || !validID(id) || decodeSupport(c, &input) != nil {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := supportActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	grant, err := h.service.Revoke(ctx, actor, workspace, id, input.Reason)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": supportGrantView(grant)})
}

func (h *SupportAccessHandler) runs(c *gin.Context) {
	configure(c)
	workspace := c.Param("workspace_id")
	if !validID(workspace) || c.Request.URL.RawQuery != "" {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := supportActor(c, h.auth, false)
	defer cancel()
	if !ok {
		return
	}
	items, err := h.service.Runs(ctx, actor, workspace)
	if err != nil {
		fail(c, err)
		return
	}
	data := make([]gin.H, 0, len(items))
	for _, item := range items {
		data = append(data, gin.H{"id": item.ID, "state": item.State, "version": strconv.FormatInt(item.Version, 10), "created_at": item.CreatedAt, "updated_at": item.UpdatedAt})
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}
