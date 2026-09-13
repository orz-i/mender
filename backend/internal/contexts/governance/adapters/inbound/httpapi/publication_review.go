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
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

const sessionCookie = "mender_session"

type PublicationReviewHandler struct {
	service *application.Service
	auth    application.Authorizer
}

func NewPublicationReview(service *application.Service, auth application.Authorizer) (*PublicationReviewHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrUnavailable
	}
	return &PublicationReviewHandler{service: service, auth: auth}, nil
}
func (h *PublicationReviewHandler) Register(router *gin.Engine) {
	base := "/api/admin/v1/workspaces/:workspace_id/publication-approvals"
	router.GET(base, h.list)
	router.POST(base+"/:approval_id/approve", h.approve)
	router.POST(base+"/:approval_id/reject", h.reject)
}
func configure(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
}
func validID(v string) bool {
	if len(v) < 1 || len(v) > 128 {
		return false
	}
	for _, ch := range v {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}
func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, application.ErrUnauthenticated):
		c.JSON(401, gin.H{"error": gin.H{"code": "UNAUTHENTICATED", "message": "Login is required."}})
	case errors.Is(err, application.ErrForbidden):
		c.JSON(403, gin.H{"error": gin.H{"code": "FORBIDDEN", "message": "Publication review is not permitted."}})
	case errors.Is(err, application.ErrInvalid):
		c.JSON(400, gin.H{"error": gin.H{"code": "INVALID_ARGUMENT", "message": "Publication review request is invalid."}})
	case errors.Is(err, application.ErrNotFound):
		c.JSON(404, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "Publication approval was not found."}})
	case errors.Is(err, application.ErrConflict):
		c.JSON(409, gin.H{"error": gin.H{"code": "CONFLICT", "message": "Publication approval is no longer reviewable."}})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(504, gin.H{"error": gin.H{"code": "TIMEOUT", "message": "Request deadline exceeded."}})
	default:
		c.JSON(503, gin.H{"error": gin.H{"code": "GOVERNANCE_UNAVAILABLE", "message": "Publication review is temporarily unavailable."}})
	}
}
func (h *PublicationReviewHandler) actor(c *gin.Context, mutation bool) (context.Context, context.CancelFunc, application.Actor, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		fail(c, application.ErrUnauthenticated)
		return ctx, cancel, application.Actor{}, false
	}
	var a application.Actor
	if mutation {
		csrf := c.GetHeader("X-Mender-CSRF")
		if csrf == "" || len(csrf) > 256 {
			fail(c, application.ErrForbidden)
			return ctx, cancel, application.Actor{}, false
		}
		a, err = h.auth.AuthenticateMutation(ctx, raw, csrf)
	} else {
		a, err = h.auth.Authenticate(ctx, raw)
	}
	if err != nil {
		fail(c, err)
		return ctx, cancel, application.Actor{}, false
	}
	return ctx, cancel, a, true
}
func nullableTime(v time.Time) any {
	if v.IsZero() {
		return nil
	}
	return v
}
func nullableString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func view(v application.PublicationApproval) gin.H {
	return gin.H{"id": v.ID, "target_kind": v.TargetKind, "target_id": v.TargetID, "target_revision": strconv.FormatInt(v.TargetRevision, 10), "requester_user_id": v.RequesterUserID, "state": v.State, "requested_at": v.RequestedAt, "expires_at": v.ExpiresAt, "reviewer_user_id": nullableString(v.ReviewerUserID), "reviewed_at": nullableTime(v.ReviewedAt), "decision_note": v.DecisionNote, "consumed_at": nullableTime(v.ConsumedAt)}
}
func (h *PublicationReviewHandler) list(c *gin.Context) {
	configure(c)
	if c.Request.URL.RawQuery != "" || !validID(c.Param("workspace_id")) {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, a, ok := h.actor(c, false)
	defer cancel()
	if !ok {
		return
	}
	items, err := h.service.List(ctx, a, c.Param("workspace_id"))
	if err != nil {
		fail(c, err)
		return
	}
	data := make([]gin.H, 0, len(items))
	for _, item := range items {
		data = append(data, view(item))
	}
	c.JSON(200, gin.H{"data": data})
}

type decisionInput struct {
	Note string `json:"note"`
}

func decode(c *gin.Context) (decisionInput, error) {
	var in decisionInput
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 4096 {
		return in, application.ErrInvalid
	}
	dec := json.NewDecoder(io.LimitReader(c.Request.Body, 4097))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return in, application.ErrInvalid
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return in, application.ErrInvalid
	}
	return in, nil
}
func (h *PublicationReviewHandler) approve(c *gin.Context) { h.decide(c, true) }
func (h *PublicationReviewHandler) reject(c *gin.Context)  { h.decide(c, false) }
func (h *PublicationReviewHandler) decide(c *gin.Context, approve bool) {
	configure(c)
	ws, id := c.Param("workspace_id"), c.Param("approval_id")
	if !validID(ws) || !validID(id) {
		fail(c, application.ErrInvalid)
		return
	}
	in, err := decode(c)
	if err != nil {
		fail(c, err)
		return
	}
	ctx, cancel, a, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	var item application.PublicationApproval
	if approve {
		item, err = h.service.Approve(ctx, a, ws, id, in.Note)
	} else {
		item, err = h.service.Reject(ctx, a, ws, id, in.Note)
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": view(item)})
}
