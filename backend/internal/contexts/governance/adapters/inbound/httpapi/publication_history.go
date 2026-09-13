package httpapi

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/governance/application"
)

type PublicationHistoryHandler struct {
	service *application.HistoryService
	auth    application.Authorizer
}

func NewPublicationHistory(service *application.HistoryService, auth application.Authorizer) (*PublicationHistoryHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrUnavailable
	}
	return &PublicationHistoryHandler{service: service, auth: auth}, nil
}

func (h *PublicationHistoryHandler) Register(router *gin.Engine) {
	router.GET("/api/admin/v1/workspaces/:workspace_id/publication-history", h.list)
}

func decimal(raw string) (int64, bool) {
	if len(raw) < 1 || len(raw) > 19 || raw[0] < '1' || raw[0] > '9' {
		return 0, false
	}
	for _, c := range raw[1:] {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	return value, err == nil && value > 0
}

func historyFilter(c *gin.Context) (application.PublicationHistoryFilter, error) {
	filter := application.PublicationHistoryFilter{Limit: 50}
	allowed := map[string]bool{"target_kind": true, "target_id": true, "approval_id": true, "event_kind": true, "before_sequence": true, "limit": true}
	values := c.Request.URL.Query()
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return filter, application.ErrInvalid
		}
	}
	filter.TargetKind = values.Get("target_kind")
	filter.TargetID = values.Get("target_id")
	filter.ApprovalID = values.Get("approval_id")
	filter.EventKind = values.Get("event_kind")
	if raw := values.Get("before_sequence"); raw != "" {
		value, ok := decimal(raw)
		if !ok {
			return filter, application.ErrInvalid
		}
		filter.BeforeSequence = value
	}
	if raw := values.Get("limit"); raw != "" {
		value, ok := decimal(raw)
		if !ok || value > 100 {
			return filter, application.ErrInvalid
		}
		filter.Limit = int(value)
	}
	return filter, nil
}

func auditView(event application.PublicationAuditEvent) gin.H {
	var observed any
	if event.ObservedRevision > 0 {
		observed = strconv.FormatInt(event.ObservedRevision, 10)
	}
	return gin.H{
		"sequence": strconv.FormatInt(event.Sequence, 10), "approval_id": nullableString(event.ApprovalID),
		"target_kind": event.TargetKind, "target_id": event.TargetID, "target_revision": strconv.FormatInt(event.TargetRevision, 10),
		"observed_revision": observed, "event_kind": event.EventKind, "actor_user_id": nullableString(event.ActorUserID),
		"occurred_at": event.OccurredAt, "reason_code": event.ReasonCode, "note": event.Note,
	}
}

func (h *PublicationHistoryHandler) list(c *gin.Context) {
	configure(c)
	workspace := c.Param("workspace_id")
	if !validID(workspace) {
		fail(c, application.ErrInvalid)
		return
	}
	filter, err := historyFilter(c)
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
	events := make([]gin.H, 0, len(page.Events))
	for _, event := range page.Events {
		events = append(events, auditView(event))
	}
	var next any
	if page.NextBeforeSequence > 0 {
		next = strconv.FormatInt(page.NextBeforeSequence, 10)
	}
	c.JSON(200, gin.H{"data": gin.H{"events": events, "next_before_sequence": next}})
}
