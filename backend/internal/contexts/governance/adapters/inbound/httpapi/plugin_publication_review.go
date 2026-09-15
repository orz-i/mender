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

type PluginPublicationReviewHandler struct {
	service *application.PluginPublicationReview
	auth    application.Authorizer
}

func NewPluginPublicationReview(service *application.PluginPublicationReview, auth application.Authorizer) (*PluginPublicationReviewHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrUnavailable
	}
	return &PluginPublicationReviewHandler{service: service, auth: auth}, nil
}

func (h *PluginPublicationReviewHandler) Register(router *gin.Engine) {
	base := "/api/admin/v1/workspaces/:workspace_id/plugin-publication-approvals"
	router.GET(base, h.list)
	router.POST(base+"/:approval_id/approve", h.approve)
	router.POST(base+"/:approval_id/reject", h.reject)
}

func pluginApprovalView(v application.PluginPublicationApproval) gin.H {
	return gin.H{
		"id": v.ID, "plugin_id": v.PluginID, "version": v.Version,
		"target_revision":   strconv.FormatInt(v.TargetRevision, 10),
		"requester_user_id": v.RequesterUserID, "state": v.State,
		"requested_at": v.RequestedAt, "expires_at": v.ExpiresAt,
		"reviewer_user_id": nullableString(v.ReviewerUserID), "reviewed_at": nullableTime(v.ReviewedAt),
		"decision_note": v.DecisionNote, "consumed_at": nullableTime(v.ConsumedAt),
	}
}

func (h *PluginPublicationReviewHandler) actor(c *gin.Context, mutation bool) (context.Context, context.CancelFunc, application.Actor, bool) {
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
		actor, err = h.auth.AuthenticateMutation(ctx, raw, csrf)
	} else {
		actor, err = h.auth.Authenticate(ctx, raw)
	}
	if err != nil {
		fail(c, err)
		return ctx, cancel, application.Actor{}, false
	}
	return ctx, cancel, actor, true
}

func (h *PluginPublicationReviewHandler) list(c *gin.Context) {
	configure(c)
	if c.Request.URL.RawQuery != "" || !validID(c.Param("workspace_id")) {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := h.actor(c, false)
	defer cancel()
	if !ok {
		return
	}
	items, err := h.service.List(ctx, actor, c.Param("workspace_id"))
	if err != nil {
		fail(c, err)
		return
	}
	data := make([]gin.H, 0, len(items))
	for _, item := range items {
		data = append(data, pluginApprovalView(item))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func decodePluginDecision(c *gin.Context) (decisionInput, error) {
	var input decisionInput
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 4096 {
		return input, application.ErrInvalid
	}
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, application.ErrInvalid
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return input, application.ErrInvalid
	}
	return input, nil
}

func (h *PluginPublicationReviewHandler) approve(c *gin.Context) { h.decide(c, true) }
func (h *PluginPublicationReviewHandler) reject(c *gin.Context)  { h.decide(c, false) }

func (h *PluginPublicationReviewHandler) decide(c *gin.Context, approve bool) {
	configure(c)
	workspace, id := c.Param("workspace_id"), c.Param("approval_id")
	if !validID(workspace) || !validID(id) {
		fail(c, application.ErrInvalid)
		return
	}
	input, err := decodePluginDecision(c)
	if err != nil {
		fail(c, err)
		return
	}
	ctx, cancel, actor, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	var item application.PluginPublicationApproval
	if approve {
		item, err = h.service.Approve(ctx, actor, workspace, id, input.Note)
	} else {
		item, err = h.service.Reject(ctx, actor, workspace, id, input.Note)
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": pluginApprovalView(item)})
}
