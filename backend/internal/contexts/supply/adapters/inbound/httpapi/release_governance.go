package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

type ReleaseGovernanceHandler struct {
	service *application.ReleaseGovernance
	auth    application.PublisherAuthorizer
}

func NewReleaseGovernance(service *application.ReleaseGovernance, auth application.PublisherAuthorizer) (*ReleaseGovernanceHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrPublicationUnavailable
	}
	return &ReleaseGovernanceHandler{service: service, auth: auth}, nil
}

func (h *ReleaseGovernanceHandler) Register(router *gin.Engine) {
	base := "/api/admin/v1/workspaces/:workspace_id/releases"
	router.GET(base, h.snapshot)
	router.POST(base+"/plans", h.create)
	router.POST(base+"/plans/:release_id/canary", h.canary)
	router.POST(base+"/plans/:release_id/promote", h.promote)
	router.POST(base+"/plans/:release_id/drain", h.drain)
	router.POST(base+"/plans/:release_id/rollback", h.rollback)
	router.POST(base+"/plans/:release_id/emergency-disable", h.disable)
}

func releaseActor(c *gin.Context, auth application.PublisherAuthorizer, mutation bool) (context.Context, context.CancelFunc, application.PublisherActor, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	raw, err := c.Cookie(publicationSessionCookie)
	if err != nil {
		publicationFail(c, application.ErrPublicationUnauthenticated)
		return ctx, cancel, application.PublisherActor{}, false
	}
	var actor application.PublisherActor
	if mutation {
		csrf := c.GetHeader("X-Mender-CSRF")
		if csrf == "" || len(csrf) > 256 {
			publicationFail(c, application.ErrPublicationForbidden)
			return ctx, cancel, application.PublisherActor{}, false
		}
		actor, err = auth.AuthenticateMutation(ctx, raw, csrf)
	} else {
		actor, err = auth.Authenticate(ctx, raw)
	}
	if err != nil {
		publicationFail(c, err)
		return ctx, cancel, application.PublisherActor{}, false
	}
	return ctx, cancel, actor, true
}

func releaseDecode(c *gin.Context, dst any) error {
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength > 1<<20 {
		return application.ErrPublicationInvalid
	}
	dec := json.NewDecoder(io.LimitReader(c.Request.Body, (1<<20)+1))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return application.ErrPublicationInvalid
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return application.ErrPublicationInvalid
	}
	return nil
}

func releasePlanView(v application.ReleasePlan) gin.H {
	return gin.H{"release_id": v.ID, "plugin_id": v.PluginID, "plugin_version": v.PluginVersion, "toolset_version_id": v.ToolsetVersionID, "tool_version_id": v.ToolVersionID, "provider_id": v.ProviderID, "stable_deployment_revision": v.StableDeploymentRevision, "candidate_deployment_revision": v.CandidateDeploymentRevision, "revision": strconv.FormatInt(v.Revision, 10), "state": v.State, "created_at": v.CreatedAt, "updated_at": v.UpdatedAt, "canary_started_at": nullablePublicationTime(v.CanaryStartedAt), "observation_until": nullablePublicationTime(v.ObservationUntil), "activated_at": nullablePublicationTime(v.ActivatedAt), "draining_at": nullablePublicationTime(v.DrainingAt), "rolled_back_at": nullablePublicationTime(v.RolledBackAt), "disabled_at": nullablePublicationTime(v.DisabledAt)}
}
func nullableReleaseString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func releaseRouteView(v application.ReleaseRoute) gin.H {
	return gin.H{"toolset_version_id": v.ToolsetVersionID, "tool_version_id": v.ToolVersionID, "stable_deployment_revision": v.StableDeploymentRevision, "candidate_deployment_revision": nullableReleaseString(v.CandidateDeploymentRevision), "mode": v.Mode, "release_plan_id": nullableReleaseString(v.ReleasePlanID), "revision": strconv.FormatInt(v.Revision, 10), "updated_at": v.UpdatedAt}
}
func releaseEventView(v application.ReleaseAuditEvent) gin.H {
	return gin.H{"sequence": strconv.FormatInt(v.Sequence, 10), "release_plan_id": v.ReleasePlanID, "plan_revision": strconv.FormatInt(v.PlanRevision, 10), "route_revision": strconv.FormatInt(v.RouteRevision, 10), "event_kind": v.EventKind, "route_mode": v.RouteMode, "selected_deployment_revision": nullableReleaseString(v.SelectedDeploymentRevision), "reason": v.Reason, "occurred_at": v.OccurredAt}
}

func (h *ReleaseGovernanceHandler) snapshot(c *gin.Context) {
	publicationConfigure(c)
	ws := c.Param("workspace_id")
	if c.Request.URL.RawQuery != "" || !publicationValidID(ws) {
		publicationFail(c, application.ErrPublicationInvalid)
		return
	}
	ctx, cancel, actor, ok := releaseActor(c, h.auth, false)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.Snapshot(ctx, actor, ws)
	if err != nil {
		publicationFail(c, err)
		return
	}
	plans := make([]gin.H, 0, len(v.Plans))
	for _, x := range v.Plans {
		plans = append(plans, releasePlanView(x))
	}
	routes := make([]gin.H, 0, len(v.Routes))
	for _, x := range v.Routes {
		routes = append(routes, releaseRouteView(x))
	}
	events := make([]gin.H, 0, len(v.Events))
	for _, x := range v.Events {
		events = append(events, releaseEventView(x))
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"plans": plans, "routes": routes, "events": events}})
}

type releaseCreateInput struct {
	PluginID                    string `json:"plugin_id"`
	PluginVersion               string `json:"plugin_version"`
	ToolsetVersionID            string `json:"toolset_version_id"`
	ToolVersionID               string `json:"tool_version_id"`
	ProviderID                  string `json:"provider_id"`
	StableDeploymentRevision    string `json:"stable_deployment_revision"`
	CandidateDeploymentRevision string `json:"candidate_deployment_revision"`
	Reason                      string `json:"reason"`
}
type releaseReasonInput struct {
	Reason string `json:"reason"`
}
type releaseCanaryInput struct {
	Reason             string `json:"reason"`
	ObservationSeconds int64  `json:"observation_seconds"`
}

func (h *ReleaseGovernanceHandler) create(c *gin.Context) {
	publicationConfigure(c)
	var in releaseCreateInput
	if err := releaseDecode(c, &in); err != nil {
		publicationFail(c, err)
		return
	}
	ctx, cancel, actor, ok := releaseActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.Create(ctx, actor, c.Param("workspace_id"), application.ReleasePlanInput{PluginID: in.PluginID, PluginVersion: in.PluginVersion, ToolsetVersionID: in.ToolsetVersionID, ToolVersionID: in.ToolVersionID, ProviderID: in.ProviderID, StableDeploymentRevision: in.StableDeploymentRevision, CandidateDeploymentRevision: in.CandidateDeploymentRevision, Reason: in.Reason})
	if err != nil {
		publicationFail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": releasePlanView(v)})
}

func (h *ReleaseGovernanceHandler) canary(c *gin.Context) {
	publicationConfigure(c)
	var in releaseCanaryInput
	if err := releaseDecode(c, &in); err != nil || in.ObservationSeconds < 60 || in.ObservationSeconds > 86400 {
		publicationFail(c, application.ErrPublicationInvalid)
		return
	}
	ctx, cancel, actor, ok := releaseActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := h.service.StartCanary(ctx, actor, c.Param("workspace_id"), c.Param("release_id"), in.Reason, time.Duration(in.ObservationSeconds)*time.Second)
	if err != nil {
		publicationFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": releasePlanView(v)})
}
func (h *ReleaseGovernanceHandler) promote(c *gin.Context)  { h.reasonAction(c, h.service.Promote) }
func (h *ReleaseGovernanceHandler) drain(c *gin.Context)    { h.reasonAction(c, h.service.Drain) }
func (h *ReleaseGovernanceHandler) rollback(c *gin.Context) { h.reasonAction(c, h.service.Rollback) }
func (h *ReleaseGovernanceHandler) disable(c *gin.Context) {
	h.reasonAction(c, h.service.EmergencyDisable)
}
func (h *ReleaseGovernanceHandler) reasonAction(c *gin.Context, action func(context.Context, application.PublisherActor, string, string, string) (application.ReleasePlan, error)) {
	publicationConfigure(c)
	var in releaseReasonInput
	if err := releaseDecode(c, &in); err != nil {
		publicationFail(c, err)
		return
	}
	ctx, cancel, actor, ok := releaseActor(c, h.auth, true)
	defer cancel()
	if !ok {
		return
	}
	v, err := action(ctx, actor, c.Param("workspace_id"), c.Param("release_id"), in.Reason)
	if err != nil {
		publicationFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": releasePlanView(v)})
}
