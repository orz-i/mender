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

type PlatformAdminHandler struct {
	service *application.PlatformAdminService
	auth    application.PlatformAuthorizer
}

func NewPlatformAdmin(service *application.PlatformAdminService, auth application.PlatformAuthorizer) (*PlatformAdminHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrUnavailable
	}
	return &PlatformAdminHandler{service: service, auth: auth}, nil
}

func (h *PlatformAdminHandler) Register(router *gin.Engine) {
	base := "/api/admin/v1/platform"
	router.GET(base+"/workspaces", h.workspaces)
	router.POST(base+"/workspaces/:workspace_id/freeze", h.freezeWorkspace)
	router.POST(base+"/workspaces/:workspace_id/unfreeze", h.unfreezeWorkspace)
	router.GET(base+"/providers", h.providers)
	router.POST(base+"/providers/:provider_id/quarantine", h.quarantineProvider)
	router.POST(base+"/providers/:provider_id/restore", h.restoreProvider)
	router.GET(base+"/incidents", h.incidents)
	router.POST(base+"/incidents", h.openIncident)
	router.POST(base+"/incidents/:incident_id/resolve", h.resolveIncident)
	router.GET(base+"/audit", h.audit)
}

func decodePlatformAdmin(c *gin.Context, dst any) error {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 8192 {
		return application.ErrInvalid
	}
	dec := json.NewDecoder(io.LimitReader(c.Request.Body, 8193))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		return application.ErrInvalid
	}
	return nil
}

func platformActor(c *gin.Context, auth application.PlatformAuthorizer, mutation bool) (context.Context, context.CancelFunc, application.Actor, bool) {
	return supportActor(c, auth, mutation)
}

func platformWorkspaceView(v application.PlatformWorkspace) gin.H {
	return gin.H{"workspace_id": v.WorkspaceID, "frozen": v.Frozen, "revision": strconv.FormatInt(v.Revision, 10), "reason": v.Reason, "actor_user_id": v.ActorUserID, "updated_at": v.UpdatedAt, "created_at": v.CreatedAt}
}

func platformProviderView(v application.PlatformProvider) gin.H {
	return gin.H{"provider_id": v.ProviderID, "state": v.State, "revision": strconv.FormatInt(v.Revision, 10), "reason": v.Reason, "actor_user_id": v.ActorUserID, "updated_at": v.UpdatedAt, "deployment_count": strconv.FormatInt(v.DeploymentCount, 10), "active_deployment_count": strconv.FormatInt(v.ActiveDeploymentCount, 10)}
}

func platformIncidentView(v application.PlatformIncident) gin.H {
	return gin.H{"id": v.ID, "target_kind": v.TargetKind, "target_id": v.TargetID, "severity": v.Severity, "code": v.Code, "state": v.State,
		"opened_by_user_id": v.OpenedByUserID, "open_reason": v.OpenReason, "resolved_by_user_id": nullableString(v.ResolvedByUserID), "resolution": v.Resolution,
		"revision": strconv.FormatInt(v.Revision, 10), "opened_at": v.OpenedAt, "updated_at": v.UpdatedAt, "resolved_at": nullableTime(v.ResolvedAt)}
}

func platformAuditView(v application.PlatformAdminAuditEvent) gin.H {
	return gin.H{"sequence": strconv.FormatInt(v.Sequence, 10), "event_kind": v.EventKind, "target_kind": v.TargetKind, "target_id": v.TargetID,
		"target_revision": strconv.FormatInt(v.TargetRevision, 10), "actor_user_id": v.ActorUserID, "reason": v.Reason, "occurred_at": v.OccurredAt}
}

func (h *PlatformAdminHandler) workspaces(c *gin.Context) {
	configure(c)
	if c.Request.URL.RawQuery != "" {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := platformActor(c, h.auth, false)
	defer cancel()
	if !ok {
		return
	}
	items, err := h.service.Workspaces(ctx, actor)
	if err != nil {
		fail(c, err)
		return
	}
	data := make([]gin.H, 0, len(items))
	for _, item := range items {
		data = append(data, platformWorkspaceView(item))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

type platformStateMutationInput struct {
	ExpectedRevision string `json:"expected_revision"`
	Reason           string `json:"reason"`
}

func parsePlatformRevision(value string) (int64, bool) { return decimal(value) }

func (h *PlatformAdminHandler) workspaceMutation(c *gin.Context, frozen bool) {
	configure(c)
	workspace := c.Param("workspace_id")
	var input platformStateMutationInput
	if !validID(workspace) || decodePlatformAdmin(c, &input) != nil {
		fail(c, application.ErrInvalid)
		return
	}
	revision, valid := parsePlatformRevision(input.ExpectedRevision)
	if !valid {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := platformActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	item, err := h.service.SetWorkspaceFrozen(ctx, actor, workspace, revision, frozen, input.Reason)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": platformWorkspaceView(item)})
}

func (h *PlatformAdminHandler) freezeWorkspace(c *gin.Context)   { h.workspaceMutation(c, true) }
func (h *PlatformAdminHandler) unfreezeWorkspace(c *gin.Context) { h.workspaceMutation(c, false) }

func (h *PlatformAdminHandler) providers(c *gin.Context) {
	configure(c)
	if c.Request.URL.RawQuery != "" {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := platformActor(c, h.auth, false)
	defer cancel()
	if !ok {
		return
	}
	items, err := h.service.Providers(ctx, actor)
	if err != nil {
		fail(c, err)
		return
	}
	data := make([]gin.H, 0, len(items))
	for _, item := range items {
		data = append(data, platformProviderView(item))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func (h *PlatformAdminHandler) providerMutation(c *gin.Context, state string) {
	configure(c)
	provider := c.Param("provider_id")
	var input platformStateMutationInput
	if !validID(provider) || decodePlatformAdmin(c, &input) != nil {
		fail(c, application.ErrInvalid)
		return
	}
	revision, valid := parsePlatformRevision(input.ExpectedRevision)
	if !valid {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := platformActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	item, err := h.service.SetProviderState(ctx, actor, provider, revision, state, input.Reason)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": platformProviderView(item)})
}

func (h *PlatformAdminHandler) quarantineProvider(c *gin.Context) {
	h.providerMutation(c, "quarantined")
}
func (h *PlatformAdminHandler) restoreProvider(c *gin.Context) { h.providerMutation(c, "active") }

type platformIncidentOpenInput struct {
	TargetKind string `json:"target_kind"`
	TargetID   string `json:"target_id"`
	Severity   string `json:"severity"`
	Code       string `json:"code"`
	Reason     string `json:"reason"`
}

func (h *PlatformAdminHandler) openIncident(c *gin.Context) {
	configure(c)
	var input platformIncidentOpenInput
	if decodePlatformAdmin(c, &input) != nil {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := platformActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	item, err := h.service.OpenIncident(ctx, actor, input.TargetKind, input.TargetID, input.Severity, input.Code, input.Reason)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": platformIncidentView(item)})
}

type platformIncidentResolveInput struct {
	ExpectedRevision string `json:"expected_revision"`
	Resolution       string `json:"resolution"`
}

func (h *PlatformAdminHandler) resolveIncident(c *gin.Context) {
	configure(c)
	id := c.Param("incident_id")
	var input platformIncidentResolveInput
	if !validID(id) || decodePlatformAdmin(c, &input) != nil {
		fail(c, application.ErrInvalid)
		return
	}
	revision, valid := parsePlatformRevision(input.ExpectedRevision)
	if !valid {
		fail(c, application.ErrInvalid)
		return
	}
	ctx, cancel, actor, ok := platformActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	item, err := h.service.ResolveIncident(ctx, actor, id, revision, input.Resolution)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": platformIncidentView(item)})
}

func platformIncidentFilter(c *gin.Context) (application.PlatformIncidentFilter, error) {
	filter := application.PlatformIncidentFilter{Limit: 50}
	allowed := map[string]bool{"state": true, "target_kind": true, "target_id": true, "before_updated_at": true, "before_id": true, "limit": true}
	values := c.Request.URL.Query()
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return filter, application.ErrInvalid
		}
	}
	filter.State, filter.TargetKind, filter.TargetID, filter.BeforeID = values.Get("state"), values.Get("target_kind"), values.Get("target_id"), values.Get("before_id")
	if raw := values.Get("before_updated_at"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return filter, application.ErrInvalid
		}
		filter.BeforeUpdatedAt = parsed.UTC()
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

func (h *PlatformAdminHandler) incidents(c *gin.Context) {
	configure(c)
	filter, err := platformIncidentFilter(c)
	if err != nil {
		fail(c, err)
		return
	}
	ctx, cancel, actor, ok := platformActor(c, h.auth, false)
	defer cancel()
	if !ok {
		return
	}
	items, err := h.service.Incidents(ctx, actor, filter)
	if err != nil {
		fail(c, err)
		return
	}
	data := make([]gin.H, 0, len(items))
	for _, item := range items {
		data = append(data, platformIncidentView(item))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

func platformAuditFilter(c *gin.Context) (application.PlatformAuditFilter, error) {
	filter := application.PlatformAuditFilter{Limit: 100}
	allowed := map[string]bool{"before_sequence": true, "limit": true}
	values := c.Request.URL.Query()
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return filter, application.ErrInvalid
		}
	}
	if raw := values.Get("before_sequence"); raw != "" {
		value, ok := decimal(raw)
		if !ok {
			return filter, application.ErrInvalid
		}
		filter.BeforeSequence = value
	}
	if raw := values.Get("limit"); raw != "" {
		value, ok := decimal(raw)
		if !ok || value > 500 {
			return filter, application.ErrInvalid
		}
		filter.Limit = int(value)
	}
	return filter, nil
}

func (h *PlatformAdminHandler) audit(c *gin.Context) {
	configure(c)
	filter, err := platformAuditFilter(c)
	if err != nil {
		fail(c, err)
		return
	}
	ctx, cancel, actor, ok := platformActor(c, h.auth, false)
	defer cancel()
	if !ok {
		return
	}
	items, err := h.service.Audit(ctx, actor, filter)
	if err != nil {
		fail(c, err)
		return
	}
	data := make([]gin.H, 0, len(items))
	for _, item := range items {
		data = append(data, platformAuditView(item))
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}
